package upload

import (
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-admin/config"

	"github.com/google/uuid"
)

type localUploader struct{}

func (l *localUploader) Upload(file *multipart.FileHeader) (string, error) {
	savePath := config.Cfg.Upload.SavePath

	dir := filepath.Join(savePath, time.Now().Format("2006/01/02"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}

	ext := filepath.Ext(file.Filename)
	storageName := fmt.Sprintf("%s%s", uuid.New().String(), ext)

	fullPath := filepath.Join(dir, storageName)

	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("创建文件失败: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := dst.ReadFrom(src); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	relPath, _ := filepath.Rel(savePath, fullPath)
	relPath = filepath.ToSlash(relPath)
	return relPath, nil
}

func (l *localUploader) Delete(path string) error {
	// 防止路径穿越：path 最终来自数据库记录，若被篡改成
	// "../../config/config.yaml" 之类的值，拼接后会删到上传目录之外。
	root := filepath.Clean(config.Cfg.Upload.SavePath)
	rel := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("非法的文件路径: %s", path)
	}

	fullPath := filepath.Join(root, rel)
	// 二次校验：确保拼接结果仍在根目录之内
	if fullPath != root && !strings.HasPrefix(fullPath, root+string(filepath.Separator)) {
		return fmt.Errorf("非法的文件路径: %s", path)
	}

	return os.Remove(fullPath)
}

func (l *localUploader) GetURL(path string) string {
	return "/uploads/" + path
}
