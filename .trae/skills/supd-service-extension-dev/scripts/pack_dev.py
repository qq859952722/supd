#!/usr/bin/env python3
"""
supd 服务与扩展打包工具 (pack_dev.py)
辅助将服务或扩展目录打包为符合 supd 规范的 .tar.gz 压缩文件。
"""

import sys
import os
import tarfile
from pathlib import Path


def pack_directory(source_dir, output_file=None):
    source_dir = Path(source_dir).resolve()
    if not source_dir.is_dir():
        print(f"错误: 指定的路径并非有效目录: {source_dir}")
        sys.exit(1)

    is_svc = (source_dir / "service.yaml").exists()
    is_ext = (source_dir / "meta.yaml").exists()

    if not is_svc and not is_ext:
        print(f"错误: 目录 {source_dir.name} 中既无 service.yaml 也无 meta.yaml")
        sys.exit(1)

    kind = "service" if is_svc else "extension"
    if not output_file:
        output_file = source_dir.parent / f"{source_dir.name}.tar.gz"
    else:
        output_file = Path(output_file).resolve()

    print(f"开始打包 {kind}: {source_dir.name} -> {output_file.name}")

    # 排除的规则模式
    exclude_patterns = {".git", ".DS_Store", "__pycache__", "*.pyc", "*.tmp", "*.log", "data", "bin", "tmp"}

    def filter_tar(tarinfo):
        filename = Path(tarinfo.name).name
        if filename in exclude_patterns or any(filename.endswith(ext.lstrip("*")) for ext in exclude_patterns if ext.startswith("*")):
            return None
        return tarinfo

    with tarfile.open(output_file, "w:gz") as tar:
        tar.add(source_dir, arcname="", filter=filter_tar)

    print(f"打包成功! 产物保存至: {output_file}")


def main():
    if len(sys.argv) < 2:
        print(f"用法: python3 {sys.argv[0]} <directory_path> [output_tar_gz_path]")
        sys.exit(1)

    src = sys.argv[1]
    out = sys.argv[2] if len(sys.argv) > 2 else None
    pack_directory(src, out)


if __name__ == "__main__":
    main()
