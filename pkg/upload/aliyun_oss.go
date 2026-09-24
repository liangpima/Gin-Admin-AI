package upload

import (
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/google/uuid"
)

type aliyunOSS struct {
	client     *oss.Client
	bucket     *oss.Bucket
	domain     string
	bucketName string
}

type OSSConfig struct {
	Type      string // aliyun, local
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Domain    string
}

func newAliyunOSS(cfg OSSConfig) (*aliyunOSS, error) {
	client, err := oss.New(cfg.Endpoint, cfg.AccessKey, cfg.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("初始化OSS客户端失败: %w", err)
	}

	bucket, err := client.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("获取Bucket失败: %w", err)
	}

	return &aliyunOSS{
		client:     client,
		bucket:     bucket,
		domain:     strings.TrimRight(cfg.Domain, "/"),
		bucketName: cfg.Bucket,
	}, nil
}

func (a *aliyunOSS) Upload(file *multipart.FileHeader) (string, error) {
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

	if err := a.bucket.PutObject(objectKey, src, oss.ContentType(contentType)); err != nil {
		return "", fmt.Errorf("上传到OSS失败: %w", err)
	}

	return objectKey, nil
}

func (a *aliyunOSS) Delete(path string) error {
	objectKey := strings.TrimPrefix(path, "/")
	return a.bucket.DeleteObject(objectKey)
}

func (a *aliyunOSS) GetURL(path string) string {
	objectKey := strings.TrimPrefix(path, "/")

	if a.domain != "" {
		return a.domain + "/" + objectKey
	}

	return fmt.Sprintf("https://%s.%s/%s", a.bucketName, a.client.Config.Endpoint, objectKey)
}

func (a *aliyunOSS) GetObject(path string) (io.ReadCloser, error) {
	objectKey := strings.TrimPrefix(path, "/")
	return a.bucket.GetObject(objectKey)
}
