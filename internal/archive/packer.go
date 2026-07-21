package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathTraversal 路径穿越攻击错误
// H-03-001 修复：替换 filepath.ErrBadPattern，语义更准确
var ErrPathTraversal = errors.New("path traversal detected in archive")

const (
	// MaxUncompressedSize 限制解压后总大小（10GB，防止 tar bomb）
	// D-06-1 修复：规格 §2.12.6 上传限制 100MB，正常解压最多 500MB-1GB，10GB 为合理上限
	MaxUncompressedSize = 10 * 1024 * 1024 * 1024
	// MaxSingleFileSize 限制单个文件解压大小（1GB）
	MaxSingleFileSize = 1024 * 1024 * 1024
)

// PackDir 将目录打包为tar.gz
// REQ-F-038: 服务导出为tar.gz格式
// C-01-001 修复：显式关闭 tar/gzip 写入器并检查错误，避免生成损坏的归档
func PackDir(srcDir string, w io.Writer) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过 data/ 目录（REQ-I-006: 导出保留数据但可选）
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// D-06-01 修复：实现跳过 data/ 目录（与上方注释一致），
		// 避免导出运行时数据/潜在敏感数据
		if relPath == "data" && info.IsDir() {
			return filepath.SkipDir
		}

		// 跳过根目录本身
		if relPath == "." {
			return nil
		}

		// 创建 tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// 使用相对路径
		header.Name = relPath

		// 写入 header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// 如果是普通文件，写入内容
		if !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tw, f)
		return err
	})

	// 显式关闭 tar 写入器（写入 padding 块），错误优先于 Walk 错误
	closeErr := tw.Close()
	// 显式关闭 gzip 写入器（写入 footer：校验和+原始大小）
	gzipErr := gw.Close()

	if walkErr != nil {
		return walkErr
	}
	if closeErr != nil {
		return fmt.Errorf("close tar writer: %w", closeErr)
	}
	if gzipErr != nil {
		return fmt.Errorf("close gzip writer: %w", gzipErr)
	}
	return nil
}

// UnpackDir 将tar.gz解包到目录
// REQ-F-038: 服务导入从tar.gz解包
// D-06-1 修复：添加 MaxUncompressedSize 防护 tar bomb
// BUG-03/04 修复：自动检测并去除单一顶层目录前缀。
// 用户用 `tar -czf xxx.tar.gz dir/` 打包时，tar 包内所有条目都带顶层目录前缀（如 "my-service/service.yaml"）。
// 直接解压到 destDir 会导致 destDir/my-service/service.yaml，使服务发现规则 services/*/service.yaml 失效。
// 修复后：如果所有条目共享同一个顶层目录前缀，则去除该前缀。
func UnpackDir(r io.Reader, destDir string) error {
	// 把 r 读到临时文件，支持两遍扫描（预扫描检测前缀 + 实际解压）
	tmpFile, err := os.CreateTemp("", "supd-unpack-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create temp file for unpack: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 复制 r 到临时文件，限制最大大小（tar 包原始大小，非解压后大小）
	if _, err := io.Copy(tmpFile, io.LimitReader(r, MaxUncompressedSize+1)); err != nil {
		return fmt.Errorf("read archive to temp: %w", err)
	}
	if fi, err := tmpFile.Stat(); err == nil && fi.Size() > MaxUncompressedSize {
		return fmt.Errorf("archive size exceeds limit %d", MaxUncompressedSize)
	}

	// 第一遍：检测共同顶层目录前缀
	prefix, err := detectCommonTopPrefix(tmpFile)
	if err != nil {
		return err
	}

	// 第二遍：实际解压（header.Name 去除前缀）
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek temp file: %w", err)
	}
	return unpackFromReader(tmpFile, destDir, prefix)
}

// detectCommonTopPrefix 扫描 tar.gz，返回共同顶层目录前缀（含尾部 "/"）。
// 如果条目不共享单一顶层目录，返回空字符串。
// 示例：条目 ["my-svc/", "my-svc/service.yaml"] → 返回 "my-svc/"
//
//	条目 ["service.yaml", "run.sh"] → 返回 ""
//
// 安全：如果顶层目录是 "." 或 ".."（路径穿越尝试），返回空前缀，
// 让 unpackFromReader 的路径检查捕获错误。
func detectCommonTopPrefix(f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek temp file: %w", err)
	}
	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var topDir string
	first := true
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		// 使用 header.Name 而非 filepath.Clean 后的字符串，
		// 因为 filepath.Clean 会去除尾部 "/"，导致目录条目 "my-svc/" 变成 "my-svc"，
		// 后续 strings.IndexByte 找不到 "/" 而误判为"无顶层目录"。
		name := header.Name
		// 跳过空条目
		if name == "" {
			continue
		}
		// 规范化路径分隔符（tar 标准使用 "/"，但防御性处理）
		name = strings.ReplaceAll(name, "\\", "/")
		// 去除开头的 "./"
		name = strings.TrimPrefix(name, "./")
		// 跳过根目录条目 "." 和 ""
		if name == "." || name == "" || name == "/" {
			continue
		}
		// 提取顶层路径组件
		// 例如 "my-svc/service.yaml" → "my-svc"
		//      "my-svc/sub/file.yaml" → "my-svc"
		//      "my-svc/" → "my-svc"（目录条目本身）
		//      "service.yaml" → ""（无顶层目录）
		var top string
		if idx := strings.IndexByte(name, '/'); idx > 0 {
			top = name[:idx]
		} else if idx == 0 {
			// 以 "/" 开头（绝对路径），跳过此条目不参与判断
			continue
		} else {
			// 条目直接在根，无共同前缀
			return "", nil
		}
		// 安全：拒绝 "." 或 ".." 作为顶层目录（路径穿越尝试）
		// 返回空前缀，让 unpackFromReader 的路径检查捕获并返回 ErrPathTraversal
		if top == "." || top == ".." {
			return "", nil
		}
		if first {
			topDir = top
			first = false
		} else if top != topDir {
			// 不同顶层目录，无共同前缀
			return "", nil
		}
	}
	if topDir == "" {
		return "", nil
	}
	return topDir + "/", nil
}

// unpackFromReader 实际解压 tar.gz 到 destDir，去除指定的路径前缀。
func unpackFromReader(r io.Reader, destDir, prefix string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var totalUsed int64 = 0

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// 去除共同前缀
		entryName := header.Name
		if prefix != "" {
			entryName = strings.TrimPrefix(header.Name, prefix)
			if entryName == header.Name {
				// 条目不在前缀下（不应发生，但防御性处理）
				return fmt.Errorf("%w: entry %q not under expected prefix %q", ErrPathTraversal, header.Name, prefix)
			}
			if entryName == "" {
				// 这是顶层目录条目本身，跳过
				continue
			}
		}

		// 安全检查：防止路径穿越
		target := filepath.Join(destDir, entryName)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			// H-03-001 修复：返回语义准确的错误而非 filepath.ErrBadPattern
			return fmt.Errorf("%w: %s", ErrPathTraversal, header.Name)
		}

		// D-06-1 修复：检查单文件大小
		if header.Size > MaxSingleFileSize {
			return fmt.Errorf("file %s size %d exceeds single file limit %d", header.Name, header.Size, MaxSingleFileSize)
		}
		// 检查累计总大小
		if totalUsed+header.Size > MaxUncompressedSize {
			return fmt.Errorf("total uncompressed size exceeds limit %d (tar bomb suspected)", MaxUncompressedSize)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			// D-06-1 修复：使用 LimitReader 防止实际写入超过声明大小
			limited := &io.LimitedReader{R: tr, N: header.Size + 1}
			if _, err := io.Copy(f, limited); err != nil {
				f.Close()
				return err
			}
			totalUsed += header.Size
			if err := f.Close(); err != nil {
				return err
			}
		}
	}

	return nil
}

// ListEntries 列出tar.gz中的条目（用于导入对比）
// REQ-F-038: 导入前列出条目供用户确认
func ListEntries(r io.Reader) ([]string, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var entries []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		entries = append(entries, header.Name)
	}

	return entries, nil
}

// FileContentFromArchive 从 tar.gz 中读取指定文件的内容
// 返回文件名到内容的映射，用于解析 service.yaml / meta.yaml 获取版本信息
func FileContentFromArchive(r io.Reader, names []string) (map[string][]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	result := make(map[string][]byte)

	// 构建 basename 集合用于匹配
	want := make(map[string]bool)
	for _, n := range names {
		want[filepath.Base(n)] = true
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		base := filepath.Base(header.Name)
		if want[base] {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			result[header.Name] = data
		}
	}

	return result, nil
}
