package upload

import (
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
	if err := u.Delete(traversal); err == nil {
		t.Fatalf("路径穿越未被拦截: Delete(%q) 未返回错误", traversal)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("目录外的文件被删除了")
	}

	// 绝对路径同样应被拒绝
	if err := u.Delete(outside); err == nil {
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
	if err := u.Delete("2026/01/02/a.png"); err != nil {
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
		SetAllowedExts(".jpg,.png")
		if err := ValidateFile("a.jpg"); err != nil {
			t.Errorf(".jpg 应被允许: %v", err)
		}
		if err := ValidateFile("a.svg"); err == nil {
			t.Error("配置中删除了 .svg，应被拒绝（修复前仍会放行）")
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
