#!/usr/bin/env python3
"""
supd 服务与扩展打包工具 (pack_dev.py)

辅助将服务或扩展目录打包为符合 supd 规范的 .tar.gz 压缩文件。

打包规则与 supd 后端 `internal/archive/packer.go` 的行为对齐：
- 服务默认排除 data/ 目录（与 DefaultPackageProfile 一致）
- 扩展保留 data/（扩展 data/ 属于可分发资源）
- 强制排除临时/备份/日志文件：*.bak, *.tmp, *.log, .cache/
- 服务支持通过 package.<profile>.yaml 自定义导出规则（include/exclude/default）

用法:
    python3 pack_dev.py <directory_path> [output_tar_gz_path] [--profile <name>] [--type service|extension]
"""

import sys
import os
import fnmatch
import tarfile
import argparse
from pathlib import Path

# 强制排除项（与 supd 后端 forcedExcludes 一致）
FORCED_EXCLUDES = {"*.bak", "*.tmp", "*.log", ".cache/"}

# 扩展专属强制排除项（与 extensionForcedExcludes 一致）
EXT_FORCED_EXCLUDES = {"*.bak", "*.tmp", "*.log", ".cache/",
                       "__pycache__/", "node_modules/", ".git/", ".svn/"}

# 服务默认排除项（与 DefaultPackageProfile 一致）
SERVICE_DEFAULT_EXCLUDES = ["data/"]

# 通用排除目录（不参与打包）
COMMON_SKIP_DIRS = {".git", ".svn", "__pycache__", "node_modules", ".cache"}


def match_pattern(pattern: str, rel_path: str) -> bool:
    """复刻 supd 后端 matchPattern 语义。

    - 以 "/" 结尾：匹配目录及其所有子内容
    - 含 "/"：对完整相对路径做 fnmatch
    - 不含 "/"：对 basename 做 fnmatch
    """
    rel_path = rel_path.replace("\\", "/")
    if pattern.endswith("/"):
        d = pattern[:-1]
        return rel_path == d or rel_path.startswith(d + "/")
    if "/" in pattern:
        return fnmatch.fnmatch(rel_path, pattern)
    return fnmatch.fnmatch(os.path.basename(rel_path), pattern)


def any_pattern_match(patterns, rel_path: str) -> bool:
    return any(match_pattern(p, rel_path) for p in patterns)


def should_pack_entry(rel_path: str, profile, forced_excludes, default_mode: str) -> bool:
    """复刻 supd 后端 shouldPackEntryWithRules 语义。

    profile: dict with keys 'include', 'exclude', 'default' (any may be None)
    default_mode: 'include' 或 'exclude'，决定未匹配条目的默认行为
    """
    for p in forced_excludes:
        if match_pattern(p, rel_path):
            return False
    if profile is None:
        if default_mode == "exclude":
            return False
        return not any_pattern_match(SERVICE_DEFAULT_EXCLUDES, rel_path) if default_mode == "include" else True
    include = profile.get("include") or []
    exclude = profile.get("exclude") or []
    if default_mode == "exclude":
        return any_pattern_match(include, rel_path)
    # default: include
    return not any_pattern_match(exclude, rel_path)


def _parse_simple_profile_yaml(text: str):
    """极简 YAML 解析器，仅支持 package profile 使用的子集：
    顶层 key: value 与顶层 key: 后跟列表项 (- item)。
    优先使用 PyYAML；不可用时回退到此实现，避免强制依赖。"""
    try:
        import yaml
        return yaml.safe_load(text) or {}
    except ImportError:
        pass

    result = {}
    current_key = None
    for raw in text.splitlines():
        line = raw.rstrip()
        if not line.strip() or line.strip().startswith("#"):
            continue
        stripped = line.lstrip()
        indent = len(line) - len(stripped)
        if stripped.startswith("- ") and current_key is not None and indent > 0:
            item = stripped[2:].strip()
            if item.startswith('"') and item.endswith('"'):
                item = item[1:-1]
            elif item.startswith("'") and item.endswith("'"):
                item = item[1:-1]
            result.setdefault(current_key, []).append(item)
        elif ":" in stripped and indent == 0:
            key, _, val = stripped.partition(":")
            key = key.strip()
            val = val.strip()
            if val == "":
                current_key = key
                result[key] = []
            else:
                current_key = None
                if val.startswith('"') and val.endswith('"'):
                    val = val[1:-1]
                elif val.startswith("'") and val.endswith("'"):
                    val = val[1:-1]
                result[key] = val
    return result


def load_package_profile(svc_dir: Path, profile_name: str):
    """加载服务目录下的 package.<profile>.yaml。返回 dict 或 None。"""
    if not profile_name:
        return None
    path = svc_dir / f"package.{profile_name}.yaml"
    if not path.exists():
        print(f"错误: profile 文件不存在: {path}", file=sys.stderr)
        sys.exit(2)
    with open(path, encoding="utf-8") as f:
        data = _parse_simple_profile_yaml(f.read())
    default = data.get("default", "include")
    if default not in ("include", "exclude"):
        print(f"错误: package.default 必须为 include 或 exclude，当前: {default}", file=sys.stderr)
        sys.exit(2)
    return {
        "include": data.get("include") or [],
        "exclude": data.get("exclude") or [],
        "default": default,
    }


def pack_directory(source_dir: Path, output_file: Path = None, profile_name: str = None,
                   pack_type: str = None, force_include: str = None):
    source_dir = source_dir.resolve()
    if not source_dir.is_dir():
        print(f"错误: 指定的路径并非有效目录: {source_dir}", file=sys.stderr)
        sys.exit(1)

    is_svc = (source_dir / "service.yaml").exists()
    is_ext = (source_dir / "meta.yaml").exists()

    if not is_svc and not is_ext:
        print(f"错误: 目录 {source_dir.name} 中既无 service.yaml 也无 meta.yaml", file=sys.stderr)
        sys.exit(1)

    if pack_type is None:
        pack_type = "service" if is_svc else "extension"
    if pack_type == "service" and not is_svc:
        print(f"错误: --type service 但未找到 service.yaml", file=sys.stderr)
        sys.exit(1)
    if pack_type == "extension" and not is_ext:
        print(f"错误: --type extension 但未找到 meta.yaml", file=sys.stderr)
        sys.exit(1)

    if force_include is None:
        force_include = "service.yaml" if pack_type == "service" else "meta.yaml"

    if not output_file:
        output_file = source_dir.parent / f"{source_dir.name}.tar.gz"
    else:
        output_file = Path(output_file).resolve()

    print(f"开始打包 {pack_type}: {source_dir.name} -> {output_file.name}")

    # 解析 profile 与强制排除项
    profile = None
    default_mode = "include"
    if pack_type == "service":
        # 服务始终使用 FORCED_EXCLUDES（与后端 forcedExcludes 一致）
        forced_excludes = FORCED_EXCLUDES
        if profile_name:
            profile = load_package_profile(source_dir, profile_name)
            default_mode = profile["default"]
            print(f"使用 profile: {profile_name} (default={default_mode}, "
                  f"include={profile['include']}, exclude={profile['exclude']})")
        else:
            # 默认排除 data/（与 supd 后端 DefaultPackageProfile 一致）
            # 通过 SERVICE_DEFAULT_EXCLUDES 在 should_pack_entry 中处理
            print(f"使用默认服务导出规则: 排除 data/，强制排除 {sorted(forced_excludes)}")
    else:
        # 扩展使用 EXT_FORCED_EXCLUDES，保留 data/，无 profile 机制
        forced_excludes = EXT_FORCED_EXCLUDES
        print(f"使用扩展导出规则: 保留 data/，强制排除 {sorted(forced_excludes)}")

    packed_count = 0
    skipped_count = 0

    with tarfile.open(output_file, "w:gz") as tar:
        for root, dirs, files in os.walk(source_dir):
            # 过滤通用跳过目录
            dirs[:] = [d for d in dirs if d not in COMMON_SKIP_DIRS]
            for f in files:
                full_p = os.path.join(root, f)
                rel_p = os.path.relpath(full_p, source_dir).replace("\\", "/")

                # force_include 文件始终打包
                if rel_p == force_include:
                    tar.add(full_p, arcname=rel_p)
                    packed_count += 1
                    continue

                if should_pack_entry(rel_p, profile, forced_excludes, default_mode):
                    tar.add(full_p, arcname=rel_p)
                    packed_count += 1
                else:
                    skipped_count += 1

    print(f"打包成功! 产物保存至: {output_file}")
    print(f"  打包文件数: {packed_count}")
    print(f"  跳过文件数: {skipped_count}")


def main():
    parser = argparse.ArgumentParser(
        description="supd 服务/扩展打包工具",
        usage="python3 pack_dev.py <directory> [output] [--profile <name>] [--type service|extension]",
    )
    parser.add_argument("directory", help="服务或扩展目录路径")
    parser.add_argument("output", nargs="?", default=None, help="输出 .tar.gz 路径（可选）")
    parser.add_argument("--profile", default=None,
                        help="服务打包 profile 名称（对应 package.<name>.yaml）")
    parser.add_argument("--type", choices=["service", "extension"], default=None,
                        help="打包类型（默认自动识别）")
    args = parser.parse_args()

    pack_directory(Path(args.directory),
                   Path(args.output) if args.output else None,
                   args.profile, args.type)


if __name__ == "__main__":
    main()
