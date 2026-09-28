package upload

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
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
	// 显式给 SDK 的 HTTP 客户端设超时。
	//
	// SDK 默认用的客户端没有总超时，对象存储卡住（网络分区、被限流、
	// 大文件传到一半对端不响应）时，这个 goroutine 会一直挂着不返回，
	// 对应的 HTTP 请求也永远不结束。上限取 uploadTimeout（5 分钟），
	// 足够传完大文件，又能保证异常时一定会失败并释放连接。
	client, err := oss.New(cfg.Endpoint, cfg.AccessKey, cfg.SecretKey,
		oss.HTTPClient(&http.Client{Timeout: uploadTimeout}))
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

func (a *aliyunOSS) Upload(ctx context.Context, file *multipart.FileHeader) (string, error) {
	// 阿里云 OSS Go SDK 的 PutObject **没有 ctx 参数**，因此无法真正取消
	// 一次已在途的上传。这里至少做「调用前检查」：客户端已经断开时直接放弃，
	// 不去发起一次注定白传的请求。真正的兜底是上面给 HTTP 客户端设的超时。
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("上传已取消: %w", err)
	}

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

func (a *aliyunOSS) Delete(_ context.Context, path string) error {
	objectKey := strings.TrimPrefix(path, "/")
	return a.bucket.DeleteObject(objectKey)
}

func (a *aliyunOSS) GetURL(path string) string {
	objectKey := strings.TrimPrefix(path, "/")

	if a.domain != "" {
		return a.domain + "/" + objectKey
	}

	// 兜底拼接：Endpoint 可能是「oss-cn-hangzhou.aliyuncs.com」，
	// 也可能被写成带协议的「https://oss-cn-hangzhou.aliyuncs.com」。
	// 不剥掉协议就会拼出 `https://bucket.https://oss-...` 这种畸形直链。
	// 只有**没配 oss.domain** 时才走到这里，因此这类问题很容易在
	// 「本地配了 domain、线上没配」的差异下才暴露出来。
	host := strings.TrimRight(stripScheme(a.client.Config.Endpoint), "/")
	return fmt.Sprintf("https://%s.%s/%s", a.bucketName, host, objectKey)
}

// stripScheme 去掉 URL 里的协议前缀（若有）。
//
// 各云厂商 SDK 的 Endpoint 配置对「要不要带协议」的容忍度不一致，
// 而运维很容易统一写成带协议的形式，因此拼接前统一归一化。
func stripScheme(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		return s[i+3:]
	}
	return s
}

func (a *aliyunOSS) GetObject(path string) (io.ReadCloser, error) {
	objectKey := strings.TrimPrefix(path, "/")
	return a.bucket.GetObject(objectKey)
}
