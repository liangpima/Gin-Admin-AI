package upload

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go-admin/config"
)

// TestLocalDeleteRejectsPathTraversal 校验删除时不会越出上传目录。
//
// path 最终来自数据库记录，一旦被篡改成 ../../ 形式，
// 未做校验的 filepath.Join 会拼出上传目录之外的路径，
// 从而删掉任意有权限的文件（如 config/config.yaml）。
func TestLocalDeleteRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	config.Cfg.Upload.SavePath = root

	// 目录外的文件，删除必须被拒绝且文件仍在
	outside := filepath.Join(filepath.Dir(root), "outside-secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	defer func() { _ = os.Remove(outside) }()

	u := &localUploader{}
	traversal := "../" + filepath.Base(outside)
	if err := u.Delete(context.Background(), traversal); err == nil {
		t.Fatalf("路径穿越未被拦截: Delete(%q) 未返回错误", traversal)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("目录外的文件被删除了")
	}

	// 绝对路径同样应被拒绝
	if err := u.Delete(context.Background(), outside); err == nil {
		t.Error("绝对路径未被拦截")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("目录外的文件被删除了")
	}
}

// TestLocalDeleteAllowsNormalPath 确保校验没有误伤正常路径
func TestLocalDeleteAllowsNormalPath(t *testing.T) {
	root := t.TempDir()
	config.Cfg.Upload.SavePath = root

	target := filepath.Join(root, "2026", "01", "02")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	file := filepath.Join(target, "a.png")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	u := &localUploader{}
	if err := u.Delete(context.Background(), "2026/01/02/a.png"); err != nil {
		t.Fatalf("删除正常路径失败: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("文件未被删除")
	}
}

// TestValidateFile 校验扩展名双重校验（危险名单 + 白名单）
func TestValidateFile(t *testing.T) {
	dangerous := []string{"evil.php", "evil.phtml", "run.sh", "a.exe", "x.jsp", "s.js"}
	for _, name := range dangerous {
		if err := ValidateFile(name); err == nil {
			t.Errorf("危险文件未被拒绝: %s", name)
		}
	}

	allowed := []string{"a.jpg", "b.PNG", "c.pdf", "d.mp4"}
	for _, name := range allowed {
		if err := ValidateFile(name); err != nil {
			t.Errorf("正常文件被误拒: %s (%v)", name, err)
		}
	}

	// 既不在危险名单也不在白名单
	if err := ValidateFile("a.yaml"); err == nil {
		t.Error("非白名单文件未被拒绝")
	}
}

// TestSetAllowedExts 校验配置项 upload.allow_exts 真正生效。
//
// 早前该配置项虽已声明却从未被读取，白名单是硬编码的 ——
// 运维为收紧安全在配置里删掉 .svg 不会有任何效果，属危险的一致性陷阱。
func TestSetAllowedExts(t *testing.T) {
	orig := allowedExts
	defer func() {
		allowedExtsMu.Lock()
		allowedExts = orig
		allowedExtsMu.Unlock()
	}()

	t.Run("按配置收紧白名单", func(t *testing.T) {
		// 用 .webp 而不是 .svg 举例：`.svg` 现在**本来就不在内置默认值里**，
		// 拿它断言「配置生效」的话，即便 SetAllowedExts 完全没被接线
		// （那正是本用例要防的历史缺陷），用例照样会通过 —— 断言必须落在
		// 「一个默认允许、被配置删掉」的扩展名上才有区分力。
		SetAllowedExts(".jpg,.png")
		if err := ValidateFile("a.jpg"); err != nil {
			t.Errorf(".jpg 应被允许: %v", err)
		}
		if err := ValidateFile("a.webp"); err == nil {
			t.Error("配置里删掉了 .webp，应被拒绝（SetAllowedExts 未接线时会误放行）")
		}
	})

	t.Run("自动补前导点并忽略大小写与空格", func(t *testing.T) {
		SetAllowedExts(" JPG , .PnG ")
		if err := ValidateFile("a.jpg"); err != nil {
			t.Errorf("jpg 应被允许: %v", err)
		}
		if err := ValidateFile("b.PNG"); err != nil {
			t.Errorf("PNG 应被允许: %v", err)
		}
	})

	t.Run("空配置不清空既有白名单", func(t *testing.T) {
		// 恢复到内置默认值后再传空串：空串是「未配置，保持现状」，
		// 不能把白名单清成空集（否则所有上传都会被拒）
		allowedExtsMu.Lock()
		allowedExts = orig
		allowedExtsMu.Unlock()

		SetAllowedExts("")

		if err := ValidateFile("a.pdf"); err != nil {
			t.Errorf("空配置不应清空白名单: %v", err)
		}
		if err := ValidateFile("a.jpg"); err != nil {
			t.Errorf("空配置不应影响默认项: %v", err)
		}
	})

	t.Run("危险扩展名黑名单不受配置放宽影响", func(t *testing.T) {
		SetAllowedExts(".php,.exe,.jpg")
		if err := ValidateFile("shell.php"); err == nil {
			t.Error("即便配置里写了 .php，也必须被硬编码黑名单拦下")
		}
		if err := ValidateFile("evil.exe"); err == nil {
			t.Error("即便配置里写了 .exe，也必须被硬编码黑名单拦下")
		}
	})
}

// TestSVGNotAllowedByDefault SVG 默认禁止上传，但可以由配置显式开启。
//
// 为什么两头都要钉：
//   - **默认禁止**：`.svg` 可以内嵌 `<script>` 与 `<foreignObject>`。本地存储时
//     `/uploads` 有 `UploadSecurity()` 的 CSP `sandbox` 兜着，但**对象存储部署下
//     `GetURL()` 返回的是不经过后端的直链**，那层头不存在 → 上传者控制的同源文档
//     = 存储型 XSS。所以它不该出现在默认白名单里。
//   - **可显式开启**：它也不能进 `dangerousExts` 黑名单（黑名单不随配置放宽），
//     否则有些确实需要矢量图标的部署就再没有出路了。要留一条**可逆**的口子
//     （改一行 `upload.allow_exts`）。
//
// ⚠️ 本文件的用例会改写包级 `allowedExts`，因此**不能** t.Parallel。
func TestSVGNotAllowedByDefault(t *testing.T) {
	orig := allowedExts
	defer func() {
		allowedExtsMu.Lock()
		allowedExts = orig
		allowedExtsMu.Unlock()
	}()

	// 显式恢复到内置默认值（前面的用例会改它；它们各自都有 defer 还原，
	// 这里再显式设一次，避免用例顺序变化时结论漂移）
	allowedExtsMu.Lock()
	allowedExts = orig
	allowedExtsMu.Unlock()

	t.Run("内置默认白名单不含 .svg", func(t *testing.T) {
		if err := ValidateFile("logo.svg"); err == nil {
			t.Error(".svg 不在默认白名单里，必须被拒绝（它能内嵌 <script>，而对象存储直链不过后端、没有 CSP sandbox）")
		}
	})

	t.Run("不把 .svg 放进危险黑名单（否则配置永远加不回来）", func(t *testing.T) {
		if dangerousExts[".svg"] {
			t.Error(".svg 不应进危险扩展名黑名单 —— 黑名单不随配置放宽，那会让它无法被显式开启")
		}
	})

	t.Run("配置里显式加回即可上传", func(t *testing.T) {
		SetAllowedExts(".jpg,.svg")
		if err := ValidateFile("logo.svg"); err != nil {
			t.Errorf("配置显式允许 .svg 后应当放行: %v", err)
		}
	})
}
