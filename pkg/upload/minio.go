package upload

import (
	"context"
	"fmt"
	"mime/multipart"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type minioUploader struct {
	client     *minio.Client
	bucket     string
	domain     string
	useSSL     bool
}

func newMinIO(cfg OSSConfig) (*minioUploader, error) {
	endpoint := cfg.Endpoint
	useSSL := true

	if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		useSSL = false
	} else if strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimPrefix(endpoint, "https://")
	} else {
		endpoint = strings.TrimSuffix(endpoint, ":9000")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化MinIO客户端失败: %w", err)
	}

	// 初始化自检也要有上限：MinIO 不可达时若无限等待，
	// 进程会卡在启动阶段不返回（现象是「起来了但端口不监听」）。
	ctx, cancel := context.WithTimeout(context.Background(), remoteInitTimeout)
	defer cancel()

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("检查Bucket失败: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("创建Bucket失败: %w", err)
		}
	}

	return &minioUploader{
		client: client,
		bucket: cfg.Bucket,
		domain: strings.TrimRight(cfg.Domain, "/"),
		useSSL: useSSL,
	}, nil
}

func (m *minioUploader) Upload(ctx context.Context, file *multipart.FileHeader) (string, error) {
	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer func() { _ = src.Close() }()

	ext := ""
	if i := strings.LastIndex(file.Filename, "."); i >= 0 {
		ext = file.Filename[i:]
	}
	objectKey := fmt.Sprintf("uploads/%s/%s%s", time.Now().Format("2006/01/02"), uuid.New().String(), ext)

	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 透传调用方的 ctx（MinIO SDK 支持），客户端断开时取消在途上传
	_, err = m.client.PutObject(ctx, m.bucket, objectKey, src, file.Size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("上传到MinIO失败: %w", err)
	}

	return objectKey, nil
}

func (m *minioUploader) Delete(ctx context.Context, path string) error {
	objectKey := strings.TrimPrefix(path, "/")
	return m.client.RemoveObject(ctx, m.bucket, objectKey, minio.RemoveObjectOptions{})
}

func (m *minioUploader) GetURL(path string) string {
	objectKey := strings.TrimPrefix(path, "/")

	if m.domain != "" {
		return m.domain + "/" + objectKey
	}

	scheme := "https"
	if !m.useSSL {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s/%s", scheme, m.client.EndpointURL().Host, objectKey)
}
