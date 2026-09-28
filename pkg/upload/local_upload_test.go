package upload

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-admin/config"

	"github.com/gin-gonic/gin"
)

// 本地存储与上传调度器的测试（P2-1）。
//
// pkg/upload 此前只有「扩展名校验 + 删除时的路径穿越」有覆盖，而**真正的写盘路径
// 一次都没被执行过** —— 覆盖率 14.3%，是本仓库最低的包。
// 本地存储又是默认后端（未配置云存储时就是它），所以这条路径恰恰是绝大多数
// 部署实际在走的：写盘失败、文件名处理不当、目录结构变化都不会有人发现。
//
// 云存储后端（aliyun / tencent / minio）需要真实凭据与网络，不在单测范围内；
// 这里覆盖的是「调度器如何选择后端」以及本地后端本身。

// withSavePath 把上传目录指到临时目录，并在用例结束时还原。
// localUploader 直接读 config.Cfg.Upload.SavePath，测试必须隔离。
func withSavePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := config.Cfg.Upload.SavePath
	config.Cfg.Upload.SavePath = dir
	t.Cleanup(func() { config.Cfg.Upload.SavePath = prev })
	return dir
}

// multipartHeader 构造一个真实的 *multipart.FileHeader。
//
// 不手搓结构体：Upload 内部会调用 file.Open() 读内容，
// 手工构造的 FileHeader 打不开，测不到真正的读写路径。
func multipartHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("构造表单失败: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("写入表单内容失败: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭表单失败: %v", err)
	}

	form, err := multipart.NewReader(bytes.NewReader(body.Bytes()), mw.Boundary()).
		ReadForm(int64(body.Len()) + 1024)
	if err != nil {
		t.Fatalf("解析表单失败: %v", err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })

	files := form.File["file"]
	if len(files) == 0 {
		t.Fatal("表单里没有文件")
	}
	return files[0]
}

// TestLocalUploadWritesFileAndReturnsRelativePath 本地存储的完整写盘路径。
//
// 断言四件事，每一件都有真实后果：
//   - 返回的是**相对路径**（数据库里存的就是它，绝对路径会把部署目录泄漏到接口上）
//   - 按日期分目录（否则单目录文件数会无限增长）
//   - 落盘文件名是 UUID + 原扩展名（用户可控的文件名绝不能直接落到磁盘上）
//   - 内容与上传的一致（中途被截断/编码破坏都会在这里暴露）
func TestLocalUploadWritesFileAndReturnsRelativePath(t *testing.T) {
	root := withSavePath(t)
	content := []byte("fake-png-bytes-for-test")

	fh := multipartHeader(t, "我的照片.png", content)
	rel, err := (&localUploader{}).Upload(context.Background(), fh)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	if filepath.IsAbs(rel) {
		t.Errorf("应返回相对路径，实际 %q", rel)
	}
	if !strings.HasPrefix(rel, "20") || strings.Count(rel, "/") != 3 {
		t.Errorf("应按 年/月/日 分目录，实际 %q", rel)
	}
	if ext := filepath.Ext(rel); ext != ".png" {
		t.Errorf("应保留原扩展名 .png，实际 %q", ext)
	}
	if strings.Contains(rel, "我的照片") {
		t.Errorf("落盘名不应包含用户提供的文件名（可被用于路径/编码攻击），实际 %q", rel)
	}

	full := filepath.Join(root, filepath.FromSlash(rel))
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("文件未落盘: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("文件内容不一致: %q", got)
	}
}

// TestLocalUploadGeneratesUniqueNames 同名文件多次上传不能互相覆盖。
//
// 用 UUID 而不是「时间戳 + 原名」的原因就在这里：同一秒内两次上传
// 若撞名，后一次会静默覆盖前一次，用户拿到的是别人的图。
func TestLocalUploadGeneratesUniqueNames(t *testing.T) {
	withSavePath(t)

	seen := make(map[string]struct{})
	for i := 0; i < 20; i++ {
		rel, err := (&localUploader{}).Upload(context.Background(), multipartHeader(t, "same.png", []byte("x")))
		if err != nil {
			t.Fatalf("第 %d 次上传失败: %v", i, err)
		}
		if _, dup := seen[rel]; dup {
			t.Fatalf("出现重名路径（会互相覆盖）: %q", rel)
		}
		seen[rel] = struct{}{}
	}
}

// TestLocalUploadCreatesMissingDirectories 目标目录不存在时要自动创建。
//
// 跨天/跨月后的第一次上传就落在这条路径上：目录不存在又不创建，
// 表现是「每天第一次上传必失败」，很容易被误判成偶发问题。
func TestLocalUploadCreatesMissingDirectories(t *testing.T) {
	root := withSavePath(t)

	rel, err := (&localUploader{}).Upload(context.Background(), multipartHeader(t, "a.jpg", []byte("x")))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(rel)))
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("日期目录应被自动创建: %v", err)
	}
}

// TestLocalUploadNoExtKeepsWorking 没有扩展名的文件不应报错（是否放行由白名单决定）。
func TestLocalUploadNoExtKeepsWorking(t *testing.T) {
	withSavePath(t)

	rel, err := (&localUploader{}).Upload(context.Background(), multipartHeader(t, "noext", []byte("x")))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if filepath.Ext(rel) != "" {
		t.Errorf("无扩展名时不应凭空补一个: %q", rel)
	}
}

// TestLocalGetURL 本地存储的访问地址：走 /uploads 静态路由（带安全响应头）。
func TestLocalGetURL(t *testing.T) {
	u := &localUploader{}
	if got := u.GetURL("2026/09/24/a.png"); got != "/uploads/2026/09/24/a.png" {
		t.Errorf("URL 拼接错误: %q", got)
	}
}

// TestInitSelectsLocalForUnknownOrEmptyType 未配置或配置了未知类型时使用本地存储。
//
// 这是默认部署走的路径（启动日志里的「[upload] 使用本地存储」）。
func TestInitSelectsLocalForUnknownOrEmptyType(t *testing.T) {
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })

	for _, typ := range []string{"", "unknown-cloud", "LOCAL"} {
		Init(map[string]string{"type": typ})
		if _, ok := getUploader().(*localUploader); !ok {
			t.Errorf("type=%q 应使用本地存储，实际 %T", typ, getUploader())
		}
	}
}

// TestInitFallsBackToLocalWhenCloudConfigInvalid 云存储初始化失败时回退本地存储。
//
// 关键在「回退」而不是「上传模块不可用」：配置填错的后果应该是
// 「文件存到了本地」（用户能看见、可修），而不是全站上传直接 500。
func TestInitFallsBackToLocalWhenCloudConfigInvalid(t *testing.T) {
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })

	// 三类云存储都给一份「类型对但参数空」的配置
	for _, typ := range []string{"aliyun", "tencent", "minio"} {
		Init(map[string]string{"type": typ})
		if _, ok := getUploader().(*localUploader); !ok {
			t.Errorf("type=%q 配置非法时应回退本地存储，实际 %T", typ, getUploader())
		}
	}
}

// markerUploader 一个带标记的假后端，用来观察「实现是否真的被替换」。
//
// 为什么不直接用 &localUploader{} 做指针比较：localUploader 是**空结构体**，
// Go 对所有零大小分配都返回同一个地址（runtime.zerobase），
// 于是 `&localUploader{} == &localUploader{}` 恒为 true ——
// 用指针相等判断「是否被替换」会永远失败。这是个容易踩的坑，记在这里。
type markerUploader struct{ name string }

func (m *markerUploader) Upload(context.Context, *multipart.FileHeader) (string, error) {
	return m.name, nil
}
func (m *markerUploader) Delete(context.Context, string) error { return nil }
func (m *markerUploader) GetURL(string) string                 { return m.name }

// TestSetAndGetUploaderAreWired setUploader / getUploader 的读写配对。
func TestSetAndGetUploaderAreWired(t *testing.T) {
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })

	setUploader(&markerUploader{name: "marker"})
	got, ok := getUploader().(*markerUploader)
	if !ok || got.name != "marker" {
		t.Fatalf("替换后应读到新实现，实际 %T", getUploader())
	}
}

// TestReloadReplacesUploader 保存配置后重建实现（管理员改 OSS 配置的路径）。
//
// 断言的是「类型发生变化」而不是指针不同 —— 见 markerUploader 的注释。
func TestReloadReplacesUploader(t *testing.T) {
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })

	// 先放一个可识别的实现，再 Reload，确认它被换掉了
	setUploader(&markerUploader{name: "old"})
	Reload(map[string]string{"type": ""})

	if _, stillOld := getUploader().(*markerUploader); stillOld {
		t.Error("Reload 应重建上传实现，否则改了配置仍走旧后端")
	}
	if _, ok := getUploader().(*localUploader); !ok {
		t.Errorf("Reload 后应为本地存储，实际 %T", getUploader())
	}
}

// TestDispatcherRequiresInit 未初始化时给出可读错误而不是 panic。
//
// 启动顺序出错（例如配置加载失败）时会走到这里，
// 「上传模块未初始化」能直接指向根因，panic 则只剩一个 500。
func TestDispatcherRequiresInit(t *testing.T) {
	prev := getUploader()
	setUploader(nil)
	t.Cleanup(func() { setUploader(prev) })

	fh := multipartHeader(t, "a.png", []byte("x"))

	if _, err := Upload(fh); err == nil || !strings.Contains(err.Error(), "未初始化") {
		t.Errorf("未初始化时 Upload 应返回可读错误，实际: %v", err)
	}
	if err := Delete("a.png"); err == nil || !strings.Contains(err.Error(), "未初始化") {
		t.Errorf("未初始化时 Delete 应返回可读错误，实际: %v", err)
	}
	// GetURL 是渲染用的，不能因为未初始化就 panic 或返回空串
	if got := GetURL("a.png"); got != "/uploads/a.png" {
		t.Errorf("未初始化时 GetURL 应降级为 /uploads 前缀，实际 %q", got)
	}
}

// TestDispatcherValidatesBeforeWriting 调度器必须先校验扩展名再落盘。
//
// 校验顺序错了（先写后校验）会在磁盘上留下一个不允许的文件 ——
// 即使接口返回失败，文件也已经能被 /uploads 访问到。
func TestDispatcherValidatesBeforeWriting(t *testing.T) {
	root := withSavePath(t)
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })
	Init(map[string]string{"type": ""})

	if _, err := Upload(multipartHeader(t, "shell.php", []byte("<?php ?>"))); err == nil {
		t.Fatal("危险扩展名必须被拒绝")
	}

	// 目录里不应留下任何文件
	var found []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			found = append(found, p)
		}
		return nil
	})
	if len(found) != 0 {
		t.Errorf("被拒绝的上传不应落盘，实际留下: %v", found)
	}
}

// TestDispatcherUploadThroughLocal 调度器 + 本地存储的完整链路。
func TestDispatcherUploadThroughLocal(t *testing.T) {
	root := withSavePath(t)
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })
	Init(map[string]string{"type": ""})

	rel, err := Upload(multipartHeader(t, "文档.pdf", []byte("pdf")))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Errorf("文件应已落盘: %v", err)
	}

	// 删除后文件消失（Delete 调度器同样要走通）
	if err := Delete(rel); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Error("删除后文件应不存在")
	}

	if got := GetURL(rel); !strings.HasPrefix(got, "/uploads/") {
		t.Errorf("GetURL 应指向 /uploads 静态路由，实际 %q", got)
	}
}

// ctxCaptureUploader 记录收到的上下文，用于验证「ctx 是否真的透传到存储实现」。
type ctxCaptureUploader struct {
	uploadCtx context.Context
	deleteCtx context.Context
}

func (c *ctxCaptureUploader) Upload(ctx context.Context, _ *multipart.FileHeader) (string, error) {
	c.uploadCtx = ctx
	return "captured", nil
}

func (c *ctxCaptureUploader) Delete(ctx context.Context, _ string) error {
	c.deleteCtx = ctx
	return nil
}

func (c *ctxCaptureUploader) GetURL(string) string { return "captured" }

// ctxMarkerKey 用一个私有类型做 key，避免与其它 ctx value 冲突。
type ctxMarkerKey struct{}

// TestUploadWithContextPropagatesRequestContext 请求上下文必须真的透传到存储实现。
//
// 此前 UploadWithContext 的实现是 `return Upload(file)`：签名承诺透传 ctx、
// 实现却把它丢掉了，于是「客户端断开就取消上传」这条能力形同虚设 ——
// 用户关掉页面后，服务端仍会把整个文件传完对象存储才释放连接。
//
// 只断言「上传成功」是区分不出透传与否的（旧实现同样成功），
// 因此这里用一个记录 ctx 的假后端，直接比较收到的 ctx 是不是请求的 ctx。
func TestUploadWithContextPropagatesRequestContext(t *testing.T) {
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })

	cap := &ctxCaptureUploader{}
	setUploader(cap)

	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxMarkerKey{}, "marker"))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	if _, err := UploadWithContext(c, multipartHeader(t, "a.png", []byte("x"))); err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	if cap.uploadCtx == nil {
		t.Fatal("存储实现没有收到上下文")
	}
	if got := cap.uploadCtx.Value(ctxMarkerKey{}); got != "marker" {
		t.Errorf("透传的不是请求上下文（值 %v）—— UploadWithContext 可能又退回了 Upload", got)
	}
}

// TestUploadProvidesDeadlineWithoutRequestContext 没有请求上下文时也必须自带超时。
//
// 否则对象存储卡住（网络分区 / 被限流 / 传到一半对端无响应）时，
// 这个 goroutine 会永久挂住，对应的 HTTP 请求也永远不结束。
func TestUploadProvidesDeadlineWithoutRequestContext(t *testing.T) {
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })

	cap := &ctxCaptureUploader{}
	setUploader(cap)

	if _, err := Upload(multipartHeader(t, "a.png", []byte("x"))); err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if cap.uploadCtx == nil {
		t.Fatal("存储实现没有收到上下文")
	}
	if _, ok := cap.uploadCtx.Deadline(); !ok {
		t.Error("Upload 应自带超时上下文，否则远端存储卡住会永久挂住")
	}
}

// TestUploadWithContextNilFallsBackToUpload gin 上下文缺失时退回普通入口。
//
// 测试代码与非 HTTP 调用路径会传 nil，此时不能 panic。
func TestUploadWithContextNilFallsBackToUpload(t *testing.T) {
	withSavePath(t)
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })
	Init(map[string]string{"type": ""})

	rel, err := UploadWithContext(nil, multipartHeader(t, "a.png", []byte("x")))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if rel == "" {
		t.Error("应返回存储路径")
	}
}

// TestDeletePropagatesContext 删除同样要透传上下文（远端 RemoveObject 可被取消）。
func TestDeletePropagatesContext(t *testing.T) {
	prev := getUploader()
	t.Cleanup(func() { setUploader(prev) })

	cap := &ctxCaptureUploader{}
	setUploader(cap)

	ctx := context.WithValue(context.Background(), ctxMarkerKey{}, "del")
	if err := DeleteContext(ctx, "2026/01/02/a.png"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if cap.deleteCtx == nil {
		t.Fatal("存储实现没有收到上下文")
	}
	if got := cap.deleteCtx.Value(ctxMarkerKey{}); got != "del" {
		t.Errorf("DeleteContext 未透传上下文（值 %v）", got)
	}

	// Delete 入口应自带超时
	cap.deleteCtx = nil
	if err := Delete("2026/01/02/a.png"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if cap.deleteCtx == nil {
		t.Fatal("存储实现没有收到上下文")
	}
	if _, ok := cap.deleteCtx.Deadline(); !ok {
		t.Error("Delete 应自带超时上下文")
	}
}
