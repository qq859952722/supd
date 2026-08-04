#!/usr/bin/env python3
"""
supd 服务与扩展本地开发结构与规范校验工具 (升级版)
自动进行代码级精确对齐校验：包含名称匹配、可执行权限、结构体键名、锁定枚举与硬上限、env.yaml 包装层等。
"""

import sys
import os
import re
import stat
import subprocess
from pathlib import Path

# ANSI 颜色
GREEN = "\033[92m"
YELLOW = "\033[93m"
RED = "\033[91m"
BLUE = "\033[94m"
RESET = "\033[0m"

FAIL_COUNT = 0
WARN_COUNT = 0

# supd 规格锁定集合与常量
NAME_REGEX = re.compile(r"^[a-z][a-z0-9-]*$")
# versionRegex 与 internal/config/service_validate.go:14 一致（三段数字）
VERSION_REGEX = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
VALID_READINESS_TYPES = {"fd_notify", "tcp_check", "http_check", "script"}
VALID_BUTTON_STYLES = {"primary", "default", "danger"}
VALID_RESTART_POLICIES = {"always", "on-failure", "never"}
VALID_CONCURRENCY = {"replace", "serialize", "parallel"}
# shellMetaChars 与 internal/config/extension_validate.go:13-25 一致
SHELL_META_CHARS = set(";|&$`(){}\n\r")
FORBIDDEN_SIGNALS = {"TERM", "KILL", "STOP", "CONT", "SEGV", "ABRT", "BUS", "FPE", "ILL"}
ALLOWED_SIGNALS = {"HUP", "INT", "QUIT", "USR1", "USR2", "PIPE", "ALRM", "CHLD"}
VALID_SERVICE_LIFECYCLE_EVENTS = {"pre_start", "post_ready", "on_failure", "pre_stop"}
VALID_SUPD_LIFECYCLE_EVENTS = {"pre_start", "post_ready", "pre_shutdown"}
MAX_TIMEOUT_SECONDS = 1800
# debounce:Ns 的 N 上限（extension_validate.go:184）
DEBOUNCE_MAX_SECONDS = 3600
README_REQUIRED_SECTIONS = (
    "服务名称与版本",
    "目录结构与权限边界",
    "启动方式与就绪检测",
    "配置与环境变量",
    "服务级扩展与 Actions",
    "数据持久化与升级更新",
    "常用运维操作",
    "安全与备份注意事项",
)


def log_pass(msg):
    print(f"[{GREEN}PASS{RESET}] {msg}")

def log_warn(msg):
    global WARN_COUNT
    WARN_COUNT += 1
    print(f"[{YELLOW}WARN{RESET}] {msg}")


def log_fail(msg):
    global FAIL_COUNT
    FAIL_COUNT += 1
    print(f"[{RED}FAIL{RESET}] {msg}")

def log_info(msg):
    print(f"[{BLUE}INFO{RESET}] {msg}")


def validate_version(content, kind):
    """校验 version 字段格式：必须匹配 ^[0-9]+\\.[0-9]+\\.[0-9]+$（三段数字）。
    依据需求规格 §2.3.2；扩展 Go 校验已落实，服务侧在此补齐门禁。"""
    m_ver = re.search(r'^\s*version:\s*["\']?([^"\'#\s]+)["\']?', content, re.MULTILINE)
    if not m_ver:
        log_fail(f"{kind} 缺少必需的 version 字段")
        return
    version = m_ver.group(1)
    if VERSION_REGEX.match(version):
        log_pass(f"{kind} version: '{version}' 符合三段数字格式")
    else:
        log_fail(f"{kind} version: '{version}' 不匹配 ^[0-9]+\\.[0-9]+\\.[0-9]+$（如 1.0.0）")


def validate_entry_path(entry, kind):
    """校验扩展 entry 路径安全性，与 internal/config/extension_validate.go:42-62 对齐。
    支持绝对路径和配置根相对路径；禁止独立 .. 路径段、shell 元字符和冗余路径分隔符。"""
    if not entry:
        log_fail(f"{kind} entry 为空")
        return
    issues = []
    if ".." in Path(entry).parts:
        issues.append("包含独立 '..' 路径段（路径穿越）")
    for ch in entry:
        if ch in SHELL_META_CHARS:
            issues.append(f"包含 shell 元字符 '{ch}'")
            break
    # 冗余路径分隔符检查：clean 后应与原值一致
    import posixpath
    cleaned = posixpath.normpath(entry)
    if cleaned != entry:
        issues.append(f"含冗余路径分隔符（'{entry}' → '{cleaned}'）")
    if issues:
        log_fail(f"{kind} entry: '{entry}' 不安全 — {'; '.join(issues)}")
    else:
        log_pass(f"{kind} entry: '{entry}' 路径安全")


def validate_actions_block(content, kind):
    """校验 actions 列表：每个 action 必须有 id 和 label，id 唯一，button_style 枚举合法。
    与 internal/config/extension_validate.go:111-129 对齐。"""
    # 提取 actions 块（actions: 之后所有缩进行，直到遇到非缩进行如 triggers:）
    actions_match = re.search(r'^actions:\s*\n((?:[ \t]+.+\n?)+)', content, re.MULTILINE)
    if not actions_match:
        return  # actions 可选，不强制
    actions_block = actions_match.group(1)
    # 按 "  - " 分割为各 action 条目（保留 - 后的内容）
    # 每个条目以缩进 + "- " 开头，后续属性行紧跟
    raw_entries = re.split(r'\n(?=[ \t]+-[ \t])', actions_block)
    # 第一段是第一个 "- id: ..." 之前的内容（通常为空或首行）
    action_entries = []
    for raw in raw_entries:
        # 去掉每条开头的缩进和 "- "，保留属性行
        entry = re.sub(r'^[ \t]*-[ \t]?', '', raw.strip())
        if entry:
            action_entries.append(entry)
    if not action_entries:
        return
    seen_ids = set()
    dup_found = False
    for idx, entry in enumerate(action_entries):
        m_id = re.search(r'^\s*id:\s*["\']?([^"\'#\s]+)["\']?', entry, re.MULTILINE)
        m_label = re.search(r'^\s*label:\s*(.+)$', entry, re.MULTILINE)
        m_bs = re.search(r'^\s*button_style:\s*["\']?([^"\'#\s]+)["\']?', entry, re.MULTILINE)
        aid = m_id.group(1) if m_id else ""
        label = m_label.group(1).strip().strip('"\'') if m_label else ""
        bs = m_bs.group(1) if m_bs else ""
        if not aid:
            log_fail(f"{kind} actions[{idx}].id: 必填")
        elif aid in seen_ids:
            log_fail(f"{kind} actions[{idx}].id: 重复 id '{aid}'")
            dup_found = True
        else:
            seen_ids.add(aid)
        if not label:
            log_fail(f"{kind} actions[{idx}].label: 必填（id='{aid}'）")
        if bs and bs not in VALID_BUTTON_STYLES:
            log_fail(f"{kind} actions[{idx}].button_style: '{bs}' 不在 {VALID_BUTTON_STYLES} 中")
    if seen_ids and not dup_found:
        log_pass(f"{kind} actions: {len(seen_ids)} 个，id 唯一性通过")


def validate_restart_policy(content):
    """校验 restart.policy 枚举与 max_backoff_ms >= backoff_ms。
    与 internal/config/service_validate.go:229-251 对齐。"""
    m_pol = re.search(r'^\s*policy:\s*["\']?([^"\'#\s]+)["\']?', content, re.MULTILINE)
    if not m_pol:
        return  # restart 块存在但无 policy — Go 端会报错，这里仅校验枚举
    policy = m_pol.group(1)
    if policy in VALID_RESTART_POLICIES:
        log_pass(f"restart.policy: '{policy}' 枚举合法")
    else:
        log_fail(f"restart.policy: '{policy}' 不在 {VALID_RESTART_POLICIES} 中")
    # max_backoff_ms >= backoff_ms（两者均 > 0 时）
    m_bo = re.search(r'^\s*backoff_ms:\s*(\d+)', content, re.MULTILINE)
    m_mbo = re.search(r'^\s*max_backoff_ms:\s*(\d+)', content, re.MULTILINE)
    if m_bo and m_mbo:
        bo = int(m_bo.group(1))
        mbo = int(m_mbo.group(1))
        if bo > 0 and mbo > 0 and mbo < bo:
            log_fail(f"restart.max_backoff_ms ({mbo}) 必须 >= backoff_ms ({bo})")


def check_executable(filepath):
    """检查文件是否存在且具备可执行权限"""
    if not filepath.exists():
        log_fail(f"入口文件 {filepath.name} 不存在")
        return False
    st = filepath.stat()
    is_exec = bool(st.st_mode & (stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH))
    if is_exec:
        log_pass(f"入口脚本 {filepath.name} 具备可执行权限 (0{oct(st.st_mode)[-3:]})")
    else:
        log_fail(f"直接执行的入口脚本 {filepath.name} 缺失执行权限，请运行: chmod +x {filepath}")
    return is_exec


def validate_env_yaml(target_dir):
    """校验 env.yaml 是否包含强制的 env: 包装层，以及是否使用了简单键值对格式（变量不生效）"""
    env_yaml = target_dir / "env.yaml"
    if not env_yaml.exists():
        return
    
    content = env_yaml.read_text(encoding="utf-8")
    if not re.search(r"^\s*env\s*:", content, re.MULTILINE):
        log_fail(f"{target_dir.name}/env.yaml 缺失强制的 'env:' 顶层包装，环境变量将被框架静默忽略！")
    else:
        log_pass(f"{target_dir.name}/env.yaml 正确包含 'env:' 包装层")
    
    # 检测简单键值对格式：KEY: scalar_value（非结构体），value 字段将为空，变量不生效
    # 正确格式：KEY:\n    value: "..."  或  KEY:\n  value: ...
    # 错误格式：KEY: /some/path  或  KEY: 123  或  KEY: "string"
    # 注意：使用 [ \t]+ 而非 \s+ 避免跨行匹配（\s 会匹配换行符）
    simple_inline_pattern = re.compile(
        r"^\s{2}([A-Za-z_][A-Za-z0-9_]*):[ \t]+\S", re.MULTILINE
    )
    inline_matches = simple_inline_pattern.findall(content)
    if inline_matches:
        for key in inline_matches:
            log_fail(f"{target_dir.name}/env.yaml 变量 '{key}' 使用内联值格式（非结构体），Value 字段为空，变量不会注入子进程！正确格式：'{key}:' 后换行缩进写 'value: ...'")


def validate_service(service_dir):
    log_info(f"开始深入校验服务目录: {service_dir.resolve()}")
    service_yaml = service_dir / "service.yaml"
    if not service_yaml.exists():
        log_fail("未找到 service.yaml 文件")
        return False

    valid = True
    readme = service_dir / "README.md"
    if not readme.exists():
        log_fail("服务根目录缺少必需的 README.md")
        valid = False
    else:
        readme_content = readme.read_text(encoding="utf-8")
        for section in README_REQUIRED_SECTIONS:
            if not re.search(rf"^#\s+{re.escape(section)}\s*$", readme_content, re.MULTILINE):
                log_fail(f"README.md 缺少必需一级标题: {section}")
                valid = False
        if valid:
            log_pass("README.md 包含服务规范要求的一级标题")

    content = service_yaml.read_text(encoding="utf-8")

    # 1. 名字对齐
    m_name = re.search(r"^\s*name:\s*[\"']?([^\"'\s#]+)[\"']?", content, re.MULTILINE)
    if m_name:
        name = m_name.group(1)
        if name == service_dir.name:
            log_pass(f"服务名称 name: '{name}' 与目录名完全一致")
        else:
            log_fail(f"服务名称不匹配! YAML 中 name='{name}', 但目录名='{service_dir.name}'")

        if NAME_REGEX.match(name):
            log_pass(f"服务名称 name: '{name}' 符合正则 ^[a-z][a-z0-9-]*$")
        else:
            log_fail(f"服务名称 name: '{name}' 格式不合法，必须匹配 ^[a-z][a-z0-9-]*$")

    # 1b. version 格式校验（三段数字）
    validate_version(content, "service")

    # 2. Readiness 校验
    m_type = re.search(r"^\s*type:\s*[\"']?([^\"'\s#]+)[\"']?", content, re.MULTILINE)
    if m_type:
        r_type = m_type.group(1)
        if r_type in VALID_READINESS_TYPES:
            log_pass(f"Readiness 类型 '{r_type}' 符合规范")
            if r_type == "script":
                if "check:" not in content:
                    log_fail("Readiness type=script 时，键名必须为 'check:'，不能写为 'command:'")
                else:
                    log_pass("Readiness type=script 正确使用了 'check:' 键名")
            elif r_type == "tcp_check":
                if "port:" not in content:
                    log_fail("Readiness type=tcp_check 时，必须包含 'port:' 配置")
            elif r_type == "fd_notify":
                if "fd:" not in content:
                    log_fail("Readiness type=fd_notify 时，必须包含 'fd:' 配置")
            elif r_type == "http_check":
                if "url:" not in content:
                    log_fail("Readiness type=http_check 时，必须包含 'url:' 配置")
        else:
            log_fail(f"非法 Readiness 类型 '{r_type}'，必须为: {VALID_READINESS_TYPES}")

    # 3. 信号校验
    for forbidden in FORBIDDEN_SIGNALS:
        if re.search(rf"^\s*(reload|rotate_logs|graceful_quit):\s*{forbidden}", content, re.IGNORECASE | re.MULTILINE):
            log_fail(f"signals 中使用了禁止的框架保留信号: {forbidden}")

    # 3b. restart 策略校验（仅当 restart 块存在时）
    m_restart = re.search(r"^\s*restart:\s*\n((?:\s+\S.+\n?)+)", content, re.MULTILINE)
    if m_restart:
        validate_restart_policy(m_restart.group(1))

    # 4. 身份字段互斥校验（User 模式 user/group 与 UID 模式 uid/gid/groups 互斥）
    has_user = re.search(r"^\s*user:\s*\S", content, re.MULTILINE)
    has_uid = re.search(r"^\s*uid:\s*\S", content, re.MULTILINE)
    if has_user and has_uid:
        log_fail("user（User 模式）与 uid（UID 模式）不能同时指定（互斥校验）")
    elif has_uid:
        log_pass("运行身份: UID 模式（uid/gid/groups）")
        m_uid_val = re.search(r"^\s*uid:\s*(-?\d+)", content, re.MULTILINE)
        if m_uid_val and int(m_uid_val.group(1)) <= 0:
            log_fail(f"uid 必须为正整数，当前值: {m_uid_val.group(1)}（0=未设置，负数会回绕）")
        m_gid_val = re.search(r"^\s*gid:\s*(-?\d+)", content, re.MULTILINE)
        if m_gid_val and int(m_gid_val.group(1)) < 0:
            log_fail(f"gid 必须为非负整数（0=等于 uid），当前值: {m_gid_val.group(1)}")
    elif has_user:
        log_pass("运行身份: User 模式（user/group）")

    # 5. env.yaml 格式校验
    validate_env_yaml(service_dir)

    # 6. 目录布局校验（Skill 生成规范要求 bin/，根目录禁止散落业务载荷）
    check_service_layout(service_dir, content)

    # 7. 服务级扩展递归校验
    extensions_dir = service_dir / "extensions"
    if extensions_dir.is_dir():
        for ext_dir in sorted(p for p in extensions_dir.iterdir() if p.is_dir()):
            validate_extension(ext_dir)
    return valid


def check_service_layout(service_dir, service_yaml_content):
    """检查服务目录是否符合 bin/ + data/ 布局规范。"""
    bin_dir = service_dir / "bin"
    if bin_dir.is_dir():
        log_pass("服务包含必需的 bin/ 目录（符合 bin/+data 布局规范）")
    elif bin_dir.exists():
        log_fail("服务根目录的 bin 必须是目录")
    else:
        log_fail("服务根目录缺少 Skill 生成规范要求的 bin/ 目录")

    allowed_root_files = {"service.yaml", "env.yaml", "README.md"}
    root_files = [f for f in service_dir.iterdir()
                  if f.is_file() and f.name not in allowed_root_files
                  and not re.fullmatch(r"package\.[a-z][a-z0-9-]*\.yaml", f.name)]
    if root_files:
        log_fail(f"服务根目录有散落业务文件 {len(root_files)} 个，程序应移入 bin/，持久化数据应移入 data/")
        for f in root_files[:5]:
            log_fail(f"  - {f.name}")

    # 检查 command 是否指向 bin/
    m_cmd = re.search(r"^\s*command:\s*\[?(.+)", service_yaml_content, re.MULTILINE)
    if m_cmd:
        cmd_line = m_cmd.group(1).strip().rstrip("]")
        first_cmd = cmd_line.split(",")[0].strip().strip("'\"")
        # 多行 YAML 数组格式：第一项形如 "- ./bin/myapp"，去除 "- " 前缀
        if first_cmd.startswith("- "):
            first_cmd = first_cmd[2:].strip().strip("'\"")
        # 仅对路径类命令（含 / 或以 ./ 开头）检查 bin/ 布局，跳过解释器（如 python3/node）
        if "/" in first_cmd or first_cmd.startswith("."):
            if first_cmd.startswith("./bin/"):
                log_pass(f"command 指向 bin/ 目录: {first_cmd}")
            elif first_cmd.startswith("./data/"):
                log_warn(f"command 指向 data/ 目录: {first_cmd}（二进制应放在 bin/）")
            elif not first_cmd.startswith("/"):
                # 相对路径但不在 bin/ 或 data/ 下
                log_warn(f"command 使用相对路径 '{first_cmd}'，建议指向 './bin/<binary>'")

    # 检查是否有二进制文件在 data/ 中
    data_dir = service_dir / "data"
    if data_dir.exists():
        for f in data_dir.rglob("*"):
            if f.is_file():
                st = f.stat()
                if st.st_mode & (stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH):
                    log_warn(f"data/ 中有可执行文件: {f.relative_to(service_dir)}（二进制应放在 bin/）")
                    break


def validate_extension(ext_dir):
    log_info(f"开始深入校验扩展目录: {ext_dir.resolve()}")
    meta_yaml = ext_dir / "meta.yaml"
    if not meta_yaml.exists():
        log_fail("未找到 meta.yaml 文件")
        return False

    content = meta_yaml.read_text(encoding="utf-8")

    # 1. 名字对齐
    m_name = re.search(r"^\s*name:\s*[\"']?([^\"'\s#]+)[\"']?", content, re.MULTILINE)
    if m_name:
        name = m_name.group(1)
        if name == ext_dir.name:
            log_pass(f"扩展名称 name: '{name}' 与目录名完全一致")
        else:
            log_fail(f"扩展名称不匹配! YAML 中 name='{name}', 但目录名='{ext_dir.name}'")

    # 1b. version 格式校验（三段数字）
    validate_version(content, "extension")

    # 2. 入口文件权限 + 路径安全
    # runtime 为空（默认 shell/bash）→ 直接执行 entry，需要执行权限
    # runtime 非空（tjs/node/python3 等）→ BuildCommand 为 [runtimePath, entry]，通过解释器执行，无需执行权限
    # 依据：internal/extension/run_context.go:115 BuildCommand
    m_runtime = re.search(r"^\s*runtime:\s*[\"']?([^\"'\s#]+)[\"']?", content, re.MULTILINE)
    runtime = m_runtime.group(1).strip() if m_runtime else ""

    m_entry = re.search(r"^\s*entry:\s*[\"']?([^\"'\s#]+)[\"']?", content, re.MULTILINE)
    if m_entry:
        entry = m_entry.group(1)
        validate_entry_path(entry, "extension")
        configured = Path(entry)
        if configured.is_absolute():
            entry_path = configured
        elif ext_dir.parent.name == "extensions":
            entry_path = ext_dir.parent.parent / configured
        elif len(configured.parts) >= 3 and configured.parts[0] == "extensions":
            entry_path = ext_dir.joinpath(*configured.parts[2:])
        else:
            entry_path = ext_dir / configured
        if not entry_path.exists():
            log_fail(f"入口文件 {entry} 不存在")
        elif runtime:
            log_pass(f"runtime '{runtime}' 通过解释器执行，入口脚本 {entry} 无需可执行权限")
        else:
            check_executable(entry_path)

    # 3. 超时上限
    m_timeout = re.search(r"^\s*timeout_seconds:\s*(\d+)", content, re.MULTILINE)
    if m_timeout:
        timeout = int(m_timeout.group(1))
        if timeout <= MAX_TIMEOUT_SECONDS:
            log_pass(f"超时设置 timeout_seconds={timeout}s ≤ 硬上限 {MAX_TIMEOUT_SECONDS}s")
        else:
            log_fail(f"超时设置 timeout_seconds={timeout}s 超出硬上限 {MAX_TIMEOUT_SECONDS}s")

    # 4. 并发控制格式（含 debounce:Ns 上限 3600 校验）
    m_conc = re.search(r"^\s*concurrency:\s*[\"']?([^\"'\s#]+)[\"']?", content, re.MULTILINE)
    if m_conc:
        conc = m_conc.group(1)
        if conc in VALID_CONCURRENCY:
            log_pass(f"并发策略 '{conc}' 符合规范")
        elif conc.startswith("debounce:"):
            m_db = re.match(r"^debounce:(\d+)s$", conc)
            if not m_db:
                log_fail(f"debounce 并发格式非法 '{conc}'，必须为 debounce:Ns (如 debounce:5s)")
            else:
                n = int(m_db.group(1))
                if n <= 0:
                    log_fail(f"debounce 的 N 必须为正整数，当前: {n}")
                elif n > DEBOUNCE_MAX_SECONDS:
                    log_fail(f"debounce 的 N 上限为 {DEBOUNCE_MAX_SECONDS}，当前: {n}")
                else:
                    log_pass(f"并发策略 '{conc}' 符合规范（N={n} ≤ {DEBOUNCE_MAX_SECONDS}）")
        else:
            log_fail(f"非法并发策略 '{conc}'，有效值: replace/serialize/parallel/debounce:Ns")

    # 4b. ui.button_style 枚举校验（仅匹配 ui: 块下的 button_style，排除 actions[] 中的）
    m_ui_block = re.search(r"^ui:\s*\n((?:[ \t]+\S.+\n?)+)", content, re.MULTILINE)
    if m_ui_block:
        m_ui_bs = re.search(r"^[ \t]+button_style:\s*[\"']?([^\"'\s#]+)[\"']?", m_ui_block.group(1), re.MULTILINE)
        if m_ui_bs:
            ui_bs = m_ui_bs.group(1)
            if ui_bs in VALID_BUTTON_STYLES:
                log_pass(f"ui.button_style: '{ui_bs}' 枚举合法")
            else:
                log_fail(f"ui.button_style: '{ui_bs}' 不在 {VALID_BUTTON_STYLES} 中")

    # 4c. actions 校验（id 必填且唯一、label 必填、button_style 枚举）
    validate_actions_block(content, "extension")

    # 5. 身份字段互斥校验（run_as 与 run_as_uid 互斥）
    has_run_as = re.search(r"^\s*run_as:\s*\S", content, re.MULTILINE)
    has_run_as_uid = re.search(r"^\s*run_as_uid:\s*\S", content, re.MULTILINE)
    if has_run_as and has_run_as_uid:
        log_fail("run_as（User 模式）与 run_as_uid（UID 模式）不能同时指定（互斥校验）")
    elif has_run_as_uid:
        log_pass("运行身份: UID 模式（run_as_uid/run_as_gid/run_as_groups）")
        m_ruid_val = re.search(r"^\s*run_as_uid:\s*(-?\d+)", content, re.MULTILINE)
        if m_ruid_val and int(m_ruid_val.group(1)) <= 0:
            log_fail(f"run_as_uid 必须为正整数，当前值: {m_ruid_val.group(1)}（0=未设置，负数会回绕）")
        m_rgid_val = re.search(r"^\s*run_as_gid:\s*(-?\d+)", content, re.MULTILINE)
        if m_rgid_val and int(m_rgid_val.group(1)) < 0:
            log_fail(f"run_as_gid 必须为非负整数（0=等于 run_as_uid），当前值: {m_rgid_val.group(1)}")

    # 6. env.yaml 格式校验
    validate_env_yaml(ext_dir)
    return True


def run_supd_validate(target_dir):
    """尝试调用 supd CLI 进行内核级语法校验（仅支持 config.yaml，不支持服务/扩展目录）"""
    # supd validate 仅支持校验 config.yaml，不支持直接校验服务/扩展目录
    config_yaml = target_dir / "config.yaml"
    if not config_yaml.exists():
        log_info("supd CLI 内核校验: supd validate 仅支持 config.yaml，当前目录无此文件，跳过 CLI 校验（内置规则校验已完成）。")
        return

    # 从系统 PATH 或当前工作目录查找 supd 二进制
    import shutil
    supd_bin = None
    if shutil.which("supd"):
        supd_bin = "supd"
    else:
        local_supd = Path.cwd() / "supd"
        if local_supd.exists() and os.access(local_supd, os.X_OK):
            supd_bin = str(local_supd)

    if supd_bin is None:
        log_info("提示: 未检测到 supd CLI 二进制，跳过 CLI 校验。")
        return

    try:
        res = subprocess.run([supd_bin, "validate", str(config_yaml), "-o"],
                             capture_output=True, text=True, timeout=5)
        if res.returncode == 0:
            log_pass(f"supd CLI config.yaml 语法与语义校验通过 ({supd_bin} validate)")
        else:
            log_warn(f"supd CLI 校验反馈:\n{res.stdout or res.stderr}")
    except Exception as e:
        log_info(f"supd CLI 调用失败: {e}")





def main():
    if len(sys.argv) < 2:
        print(f"用法: python3 {sys.argv[0]} <service_or_extension_directory_path>")
        sys.exit(1)

    target_dir = Path(sys.argv[1])
    if not target_dir.is_dir():
        log_fail(f"指定的路径非有效目录: {target_dir}")
        sys.exit(1)

    is_svc = (target_dir / "service.yaml").exists()
    is_ext = (target_dir / "meta.yaml").exists()

    if is_svc:
        valid = validate_service(target_dir)
    elif is_ext:
        valid = validate_extension(target_dir)
    else:
        log_fail("目标目录下既无 service.yaml 也无 meta.yaml")
        sys.exit(1)

    run_supd_validate(target_dir)
    print(f"\n校验汇总: {FAIL_COUNT} 个错误, {WARN_COUNT} 个警告")
    if not valid or FAIL_COUNT > 0:
        sys.exit(1)


if __name__ == "__main__":
    main()
