package upload

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// uploadTimeout 单次上传/删除的默认时间上限。
//
// 用于**没有请求上下文可透传**的调用（启动期自检、后台任务、定时清理）。
// 有请求上下文时必须透传它（见 UploadContext）—— 客户端关掉页面 / 取消请求时，
// 出站调用能立刻被取消，而不是把整个文件传完才释放连接。
const uploadTimeout = 5 * time.Minute

// remoteInitTimeout 远端存储初始化（连通性自检）的时间上限。
//
// 启动期如果没有上限，对象存储不可达会让进程**卡在启动阶段**不返回，
// 运维看到的现象是「进程起来了但端口一直不监听」，排查方向完全跑偏。
const remoteInitTimeout = 10 * time.Second

type uploader interface {
	// ctx 由调用方透传，用于取消远端出站调用（本地实现可忽略）
	Upload(ctx context.Context, file *multipart.FileHeader) (string, error)
	Delete(ctx context.Context, path string) error
	GetURL(path string) string
}

// up 当前生效的上传实现。
// 通过 upMu 保护：管理员保存 oss.* 配置时会调用 Reload 重建实现，
// 此时可能仍有请求在并发上传，必须加锁避免数据竞争。
var (
	upMu sync.RWMutex
	up   uploader
)

// getUploader 并发安全地读取当前上传实现
func getUploader() uploader {
	upMu.RLock()
	defer upMu.RUnlock()
	return up
}

// setUploader 并发安全地替换当前上传实现
func setUploader(u uploader) {
	upMu.Lock()
	defer upMu.Unlock()
	up = u
}

// allowedExts 上传文件扩展名白名单（内置默认值，可被配置覆盖）
var (
	allowedExtsMu sync.RWMutex
	allowedExts   = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true, ".webp": true,
		".mp4": true, ".mov": true, ".avi": true, ".mkv": true, ".webm": true,
		".mp3": true, ".wav": true, ".flac": true, ".aac": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
		".ppt": true, ".pptx": true, ".zip": true, ".rar": true, ".7z": true,
		".txt": true, ".csv": true, ".json": true,
	}
)

// ⚠️ **`.svg` 不在默认白名单里，这是有意的**（2026-09-28 移除）。
//
// 原因不是「SVG 是坏格式」，而是它能否被安全地托管**完全取决于部署形态**：
//
//   - 本地存储：`/uploads` 由 `router.go` 挂了 `middleware.UploadSecurity()`，
//     它会加 `Content-Security-Policy: default-src 'none'; …; sandbox`。
//     那个 `sandbox`（不带 allow-scripts）正是挡住「SVG 内嵌 <script>」的东西，
//     SVG 里还可以塞 `<foreignObject>` + HTML，同样被它拦下。
//   - **对象存储（aliyun / cos / minio）**：`fileService.Upload` 存的是
//     `upload.GetURL()` 返回的**直链**（`pkg/upload/*.go` 的 GetURL），
//     请求根本不经过后端 —— 上面那两层响应头不存在，
//     访问该直链就是一个由上传者控制的同源文档 = **存储型 XSS**。
//
// 也就是说「靠响应头兜住」这层防护会随部署方式静默消失，而对象存储是本项目
// 明确支持的部署方式。既然 SVG 在本项目里**没有任何必需用途**（站点 logo、
// 图标、种子数据都不依赖上传 SVG），就不该把这份风险留在默认配置里。
//
// 需要它时的两条出路：
//   1. 运维在 `upload.allow_exts` 里显式加回 `.svg`（**可逆，一行配置**）——
//      但前提是确认部署形态下 `/uploads` 一定经过后端，或用了独立域名 + 收紧 CSP；
//   2. 更好：需要矢量图标就用内联 SVG 组件（见 `web/src/utils/icons.ts` 的
//      `appIcons` 白名单），完全不走上传链路。
//
// 注意这份内置默认值与 `config/config.yaml` 的 `upload.allow_exts` 是**两份数据**，
// 两边都要改，否则「改了配置没生效」或反之 —— 见下面 SetAllowedExts 的说明。
// 已用 pkg/upload 的用例把「默认不含 .svg」和「配置可显式加回」两头钉住。

// SetAllowedExts 用配置项 upload.allow_exts 覆盖白名单（逗号分隔）。
//
// 早前这个配置项虽已声明却从未被读取，实际白名单是硬编码的 ——
// 运维为了收紧安全在配置里删掉某个扩展名，不会有任何效果，属危险的一致性陷阱。
//
// 传入空串表示沿用内置默认值。注意这只是「白名单」，
// dangerousExts 那份硬编码黑名单始终生效，不随配置放宽
// （所以 `.php` 这类即便被写进配置也进不来）。
//
// ⚠️ 配置里的值会**整体替换**内置默认值，不是「追加」：
// 想让默认项继续可用就必须把它们一并写全（见 config/config.yaml 的实例）。
func SetAllowedExts(csv string) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return
	}

	next := make(map[string]bool)
	for _, part := range strings.Split(csv, ",") {
		ext := strings.ToLower(strings.TrimSpace(part))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		next[ext] = true
	}
	if len(next) == 0 {
		return
	}

	allowedExtsMu.Lock()
	allowedExts = next
	allowedExtsMu.Unlock()
}

// dangerousExts 危险文件扩展名（双重校验）
var dangerousExts = map[string]bool{
	".php": true, ".php3": true, ".php5": true, ".phtml": true,
	".asp": true, ".aspx": true, ".jsp": true, ".jspx": true,
	".sh": true, ".bash": true, ".bat": true, ".cmd": true,
	".exe": true, ".dll": true, ".so": true, ".com": true,
	".js": true, ".vbs": true, ".wsf": true, ".scr": true,
}

// Logf 上传模块的日志出口。
//
// 默认写标准库 log，而不是直接 import internal/logger ——
// 本项目有一条硬约束：**pkg/ 不得反向依赖 internal/**（见 AGENTS.md 与
// pkg/task.SetPanicHandler 的同类做法），因此这里留出注入点，
// 由 cmd/server/main.go 接到项目日志上。
//
// 为什么这个注入点很关键：OSS/COS/MinIO 初始化失败会**静默回退到本地存储**，
// 这是「对象存储配置写错、但服务照常启动、文件全落到本地磁盘」的典型场景。
// 只写标准库 log 的话它只出现在容器 stdout 的一行里，
// 而生产上几乎没人会去翻 —— 接到项目日志（含文件轮转与告警）才算真的可见。
var Logf = func(format string, args ...interface{}) {
	log.Printf(format, args...)
}

// SetLogger 注入日志实现，应在 Init 之前调用。
// 传 nil 表示保持默认（写标准库 log），与 pkg/task.SetPanicHandler 一致。
func SetLogger(f func(format string, args ...interface{})) {
	if f != nil {
		Logf = f
	}
}

// Init 初始化上传模块，cfgMap 为 oss.* 配置的 key-value map
func Init(cfgMap map[string]string) {
	cfg := OSSConfig{
		Type:      cfgMap["type"],
		Endpoint:  cfgMap["endpoint"],
		Bucket:    cfgMap["bucket"],
		AccessKey: cfgMap["access_key"],
		SecretKey: cfgMap["secret_key"],
		Domain:    cfgMap["domain"],
	}

	switch cfg.Type {
	case "aliyun":
		ossUploader, err := newAliyunOSS(cfg)
		if err != nil {
			Logf("[upload] 阿里云OSS初始化失败，回退到本地存储: %v", err)
			setUploader(&localUploader{})
			return
		}
		setUploader(ossUploader)
		Logf("[upload] 使用阿里云OSS存储, Bucket: %s", cfg.Bucket)
	case "tencent":
		cosUploader, err := newTencentCOS(cfg)
		if err != nil {
			Logf("[upload] 腾讯云COS初始化失败，回退到本地存储: %v", err)
			setUploader(&localUploader{})
			return
		}
		setUploader(cosUploader)
		Logf("[upload] 使用腾讯云COS存储, Bucket: %s", cfg.Bucket)
	case "minio":
		minioUp, err := newMinIO(cfg)
		if err != nil {
			Logf("[upload] MinIO初始化失败，回退到本地存储: %v", err)
			setUploader(&localUploader{})
			return
		}
		setUploader(minioUp)
		Logf("[upload] 使用MinIO存储, Bucket: %s", cfg.Bucket)
	default:
		setUploader(&localUploader{})
		Logf("[upload] 使用本地存储")
	}
}

// Reload 重新加载上传配置
func Reload(cfgMap map[string]string) {
	Init(cfgMap)
}

// ValidateFile 校验文件扩展名白名单
func ValidateFile(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))

	// 双重校验：先检查危险扩展名
	if dangerousExts[ext] {
		return fmt.Errorf("不允许上传 %s 类型文件", ext)
	}

	// 再检查白名单
	allowedExtsMu.RLock()
	allowed := allowedExts[ext]
	allowedExtsMu.RUnlock()

	if !allowed {
		return fmt.Errorf("不支持的文件类型 %s", ext)
	}

	return nil
}

// Upload 上传文件，使用默认超时上下文。
//
// 无请求上下文可用时用它；有请求上下文时请走 UploadContext，
// 以便客户端断开后能及时取消出站调用。
func Upload(file *multipart.FileHeader) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()
	return UploadContext(ctx, file)
}

// UploadContext 上传文件并透传上下文。
func UploadContext(ctx context.Context, file *multipart.FileHeader) (string, error) {
	u := getUploader()
	if u == nil {
		return "", fmt.Errorf("上传模块未初始化")
	}

	// 校验文件扩展名
	if err := ValidateFile(file.Filename); err != nil {
		return "", err
	}

	return u.Upload(ctx, file)
}

// UploadWithContext gin 入口：把请求上下文透传给存储实现。
//
// 此前它只是 `return Upload(file)` —— 签名承诺了透传上下文，实现却丢掉了，
// 属于「看起来有取消能力、实际没有」。现在真的透传：
// 用户取消上传时，远端 PutObject 会被 ctx 取消，连接立刻释放。
//
// c 为 nil 时（测试或非 HTTP 调用路径）退回默认超时上下文。
func UploadWithContext(c *gin.Context, file *multipart.FileHeader) (string, error) {
	if c == nil || c.Request == nil {
		return Upload(file)
	}
	return UploadContext(c.Request.Context(), file)
}

func Delete(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()
	return DeleteContext(ctx, path)
}

// DeleteContext 删除文件并透传上下文。
func DeleteContext(ctx context.Context, path string) error {
	u := getUploader()
	if u == nil {
		return fmt.Errorf("上传模块未初始化")
	}
	return u.Delete(ctx, path)
}

func GetURL(path string) string {
	u := getUploader()
	if u == nil {
		return "/uploads/" + path
	}
	return u.GetURL(path)
}
