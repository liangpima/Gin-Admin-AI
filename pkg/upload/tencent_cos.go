package upload

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tencentyun/cos-go-sdk-v5"
)

type tencentCOS struct {
	client     *cos.Client
	domain     string
	bucketName string
	region     string
}

func newTencentCOS(cfg OSSConfig) (*tencentCOS, error) {
	var baseURL *cos.BaseURL

	if cfg.Domain != "" {
		domain := cfg.Domain
		if !strings.HasPrefix(domain, "http") {
			domain = "https://" + domain
		}
		u, _ := neturl.Parse(domain)
		baseURL = &cos.BaseURL{BucketURL: u}
	} else {
		bucketURL := fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.Bucket, cfg.Endpoint)
		u, _ := neturl.Parse(bucketURL)
		baseURL = &cos.BaseURL{BucketURL: u}
	}

	client := cos.NewClient(baseURL, &http.Client{})

	_, _, err := client.Bucket.Get(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("连接COS失败: %w", err)
	}

	return &tencentCOS{
		client:     client,
		domain:     strings.TrimRight(cfg.Domain, "/"),
		bucketName: cfg.Bucket,
		region:     cfg.Endpoint,
	}, nil
}

func (t *tencentCOS) Upload(file *multipart.FileHeader) (string, error) {
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

	_, err = t.client.Object.Put(context.Background(), objectKey, src, &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: contentType,
		},
	})
	if err != nil {
		return "", fmt.Errorf("上传到COS失败: %w", err)
	}

	return objectKey, nil
}

func (t *tencentCOS) Delete(path string) error {
	objectKey := strings.TrimPrefix(path, "/")
	_, err := t.client.Object.Delete(context.Background(), objectKey)
	return err
}

func (t *tencentCOS) GetURL(path string) string {
	objectKey := strings.TrimPrefix(path, "/")

	if t.domain != "" {
		return t.domain + "/" + objectKey
	}

	return fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", t.bucketName, t.region, objectKey)
}
