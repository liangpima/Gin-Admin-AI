package service

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-admin/config"
	"go-admin/internal/common"
)

// makeCertFileHeader 把一段内容包成真实的 multipart.FileHeader。
//
// 不能手工构造 FileHeader 结构体：它的 file 字段未导出，
// `Open()` 会直接返回 ErrMissingFile。必须真的走一遍 multipart 序列化。
func makeCertFileHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("构造表单失败: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("写入表单失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭表单失败: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/x", &buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("解析表单失败: %v", err)
	}
	t.Cleanup(func() { _ = r.MultipartForm.RemoveAll() })

	return r.MultipartForm.File["file"][0]
}

// TestCertSaveDirIsOutsideUploads 证书目录绝不能落在匿名可读的静态目录内。
//
// 这是本文件最重要的一条：`uploads/` 由 router 以 `r.Static("/uploads")` 对外
// 匿名可读，把商户私钥写进去等于公开发布 —— 任何人 GET
// `/uploads/certs/xxx.key` 就能拿走支付私钥。这条断言的作用是让「有人把目录
// 改到 uploads 下」这个改动**必然失败**，而不是靠 review 时有人想起来。
func TestCertSaveDirIsOutsideUploads(t *testing.T) {
	uploadRoot := filepath.Clean(config.Cfg.Upload.SavePath)
	if uploadRoot == "" || uploadRoot == "." {
		uploadRoot = "uploads"
	}
	certDir := filepath.Clean(certSaveDir)

	if certDir == uploadRoot ||
		strings.HasPrefix(certDir, uploadRoot+string(os.PathSeparator)) {
		t.Fatalf("证书目录 %q 落在静态可读目录 %q 之内，私钥会被匿名下载", certDir, uploadRoot)
	}
}

// TestUploadCertRejectsNonCertExtension 只接受 .pem/.key/.crt/.cer。
//
// 与 pkg/upload 的业务附件白名单刻意分开：那份含图片、压缩包等，
// 套到证书上传上等于允许把任意文件写进 runtime/certs。
func TestUploadCertRejectsNonCertExtension(t *testing.T) {
	svc := NewConfigService()

	for _, name := range []string{"shell.php", "payload.sh", "pic.png", "archive.zip", "noext"} {
		_, err := svc.UploadCert(makeCertFileHeader(t, name, []byte("x")))
		if err == nil {
			t.Errorf("%s 不是证书类型，必须被拒绝", name)
			continue
		}
		if be, ok := common.AsBizError(err); !ok || be.Code != common.CodeBadRequest {
			t.Errorf("%s 应返回 400 业务错误（用户看得懂），实际 %T %v", name, err, err)
		}
	}
}

// TestUploadCertRejectsOversize 超过 2MB 的文件必须被拒。
func TestUploadCertRejectsOversize(t *testing.T) {
	t.Chdir(t.TempDir())
	svc := NewConfigService()

	big := make([]byte, certMaxSize+1)
	_, err := svc.UploadCert(makeCertFileHeader(t, "merchant.key", big))
	if err == nil {
		t.Fatal("超限文件应被拒绝")
	}
	if be, ok := common.AsBizError(err); !ok || be.Code != common.CodeBadRequest {
		t.Errorf("超限应返回 400 业务错误，实际 %T %v", err, err)
	}

	// 被拒的文件不能留下任何残骸
	if _, statErr := os.Stat(certSaveDir); !os.IsNotExist(statErr) {
		t.Errorf("被拒绝的上传不应创建目录 %s", certSaveDir)
	}
}

// TestUploadCertSavesAndIgnoresRawFilename 落盘文件名不采用原始文件名。
//
// 为什么必须自己生成文件名：原始名是**外部输入**。路径分隔符在 HTTP 层
// 已被 multipart 库 base 化（本例实测 `../../../etc/evil.key` 到 Service 时
// 已成 `evil.key`），但仍有别的花样 —— `.php.key` 这类双扩展名、
// Windows 保留设备名（`CON`、`NUL`）、超长名、以及覆盖同目录已有文件。
// 落盘名只由「时间戳 + 已校验的扩展名」拼成，这些全部不成立。
//
// 若有人图省事把落盘改成 `filepath.Join(certSaveDir, file.Filename)`，
// 下面的断言会红。
func TestUploadCertSavesAndIgnoresRawFilename(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	svc := NewConfigService()
	raw := "../../../etc/evil.key"

	res, err := svc.UploadCert(makeCertFileHeader(t, raw, []byte("PRIVATE-KEY")))
	if err != nil {
		t.Fatalf("合法证书应上传成功: %v", err)
	}

	if res.Filename == "" {
		t.Error("应回显原始文件名供界面展示")
	}

	certDir := filepath.Join(tmp, certSaveDir)
	abs, err := filepath.Abs(res.Path)
	if err != nil {
		t.Fatalf("解析路径失败: %v", err)
	}
	if !strings.HasPrefix(abs, certDir+string(os.PathSeparator)) {
		t.Fatalf("落盘路径 %q 逃出了证书目录 %q", abs, certDir)
	}

	base := filepath.Base(res.Path)
	if base == res.Filename {
		t.Errorf("落盘不应沿用原始文件名，实际 %q", base)
	}
	if !strings.HasPrefix(base, "wechat_") || filepath.Ext(base) != ".key" {
		t.Errorf("落盘名应为 wechat_<时间戳><扩展名> 形式，实际 %q", base)
	}

	// 内容必须真的写进去了（只返回路径不算保存成功）
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("读取落盘文件失败: %v", err)
	}
	if string(got) != "PRIVATE-KEY" {
		t.Errorf("落盘内容不符，实际 %q", string(got))
	}
}
