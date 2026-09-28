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
	// BucketURL 必须是 **COS 的 API 端点**，而不是 oss.domain。
	//
	// oss.domain 的语义是「对外访问域名」（通常是 CDN / 自定义加速域名），
	// 它只用于生成给前端用的直链。把它当 API 端点会让所有 Put/Delete/Get
	// 都发到 CDN 主机上 —— CDN 一般只回源 GET，上传会直接失败，
	// 表现为「配了 domain 之后反而传不上去」，而错误信息只是笼统的连接失败。
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("COS 初始化失败: 缺少 oss.endpoint（区域，如 ap-guangzhou）")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("COS 初始化失败: 缺少 oss.bucket")
	}

	// endpoint 统一剥掉协议，避免拼出 `https://b.cos.https://cos...`
	region := stripScheme(cfg.Endpoint)
	bucketURL := fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.Bucket, region)
	u, err := neturl.Parse(bucketURL)
	if err != nil {
		return nil, fmt.Errorf("COS 初始化失败: 解析 BucketURL %q 出错: %w", bucketURL, err)
	}

	// 显式给 SDK 的 HTTP 客户端设超时：默认客户端没有总超时，
	// COS 卡住时上传请求会永远挂着（理由同 aliyun_oss.go）。
	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{Timeout: uploadTimeout})

	ctx, cancel := context.WithTimeout(context.Background(), remoteInitTimeout)
	defer cancel()

	if _, _, err := client.Bucket.Get(ctx, nil); err != nil {
		return nil, fmt.Errorf("连接COS失败: %w", err)
	}

	return &tencentCOS{
		client:     client,
		domain:     strings.TrimRight(cfg.Domain, "/"),
		bucketName: cfg.Bucket,
		region:     region,
	}, nil
}

func (t *tencentCOS) Upload(ctx context.Context, file *multipart.FileHeader) (string, error) {
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

	// 透传调用方的 ctx：COS SDK 支持 ctx，客户端断开时能真正取消在途上传，
	// 不必把整个文件传完才释放连接（这是本地存储不需要、而远端存储必须做的事）。
	_, err = t.client.Object.Put(ctx, objectKey, src, &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: contentType,
		},
	})
	if err != nil {
		return "", fmt.Errorf("上传到COS失败: %w", err)
	}

	return objectKey, nil
}

func (t *tencentCOS) Delete(ctx context.Context, path string) error {
	objectKey := strings.TrimPrefix(path, "/")
	_, err := t.client.Object.Delete(ctx, objectKey)
	return err
}

func (t *tencentCOS) GetURL(path string) string {
	objectKey := strings.TrimPrefix(path, "/")

	if t.domain != "" {
		return t.domain + "/" + objectKey
	}

	return fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", t.bucketName, stripScheme(t.region), objectKey)
}
