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

# supd 规格锁定集合与常量
NAME_REGEX = re.compile(r"^[a-z][a-z0-9-]*$")
VALID_READINESS_TYPES = {"fd_notify", "tcp_check", "http_check", "script"}
VALID_BUTTON_STYLES = {"primary", "default", "danger"}
VALID_CONCURRENCY = {"replace", "serialize", "parallel"}
FORBIDDEN_SIGNALS = {"TERM", "KILL", "STOP", "CONT", "SEGV", "ABRT", "BUS", "FPE", "ILL"}
ALLOWED_SIGNALS = {"HUP", "INT", "QUIT", "USR1", "USR2", "PIPE", "ALRM", "CHLD"}
VALID_SERVICE_LIFECYCLE_EVENTS = {"pre_start", "post_ready", "on_failure", "pre_stop"}
VALID_SUPD_LIFECYCLE_EVENTS = {"pre_start", "post_ready", "pre_shutdown"}
MAX_TIMEOUT_SECONDS = 1800


def log_pass(msg):
    print(f"[{GREEN}PASS{RESET}] {msg}")

def log_warn(msg):
    print(f"[{YELLOW}WARN{RESET}] {msg}")

def log_fail(msg):
    print(f"[{RED}FAIL{RESET}] {msg}")

def log_info(msg):
    print(f"[{BLUE}INFO{RESET}] {msg}")


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
        log_warn(f"入口脚本 {filepath.name} 缺失执行权限，请运行: chmod +x {filepath}")
    return is_exec


def validate_env_yaml(target_dir):
    """校验 env.yaml 是否包含强制的 env: 包装层"""
    env_yaml = target_dir / "env.yaml"
    if not env_yaml.exists():
        return
    
    content = env_yaml.read_text(encoding="utf-8")
    if not re.search(r"^\s*env\s*:", content, re.MULTILINE):
        log_fail(f"{target_dir.name}/env.yaml 缺失强制的 'env:' 顶层包装，环境变量将被框架静默忽略！")
    else:
        log_pass(f"{target_dir.name}/env.yaml 正确包含 'env:' 包装层")


def validate_service(service_dir):
    log_info(f"开始深入校验服务目录: {service_dir.resolve()}")
    service_yaml = service_dir / "service.yaml"
    if not service_yaml.exists():
        log_fail("未找到 service.yaml 文件")
        return False

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
    return True


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

    # 2. 入口文件权限
    m_entry = re.search(r"^\s*entry:\s*[\"']?([^\"'\s#]+)[\"']?", content, re.MULTILINE)
    if m_entry:
        entry = m_entry.group(1).lstrip("./")
        check_executable(ext_dir / entry)

    # 3. 超时上限
    m_timeout = re.search(r"^\s*timeout_seconds:\s*(\d+)", content, re.MULTILINE)
    if m_timeout:
        timeout = int(m_timeout.group(1))
        if timeout <= MAX_TIMEOUT_SECONDS:
            log_pass(f"超时设置 timeout_seconds={timeout}s ≤ 硬上限 {MAX_TIMEOUT_SECONDS}s")
        else:
            log_fail(f"超时设置 timeout_seconds={timeout}s 超出硬上限 {MAX_TIMEOUT_SECONDS}s")

    # 4. 并发控制格式
    m_conc = re.search(r"^\s*concurrency:\s*[\"']?([^\"'\s#]+)[\"']?", content, re.MULTILINE)
    if m_conc:
        conc = m_conc.group(1)
        if conc in VALID_CONCURRENCY or conc.startswith("debounce:"):
            if conc.startswith("debounce:"):
                if not re.match(r"^debounce:\d+s$", conc):
                    log_fail(f"debounce 并发格式非法 '{conc}'，必须为 debounce:Ns (如 debounce:5s)")
                else:
                    log_pass(f"并发策略 '{conc}' 符合规范")
            else:
                log_pass(f"并发策略 '{conc}' 符合规范")
        else:
            log_fail(f"非法并发策略 '{conc}'，有效值: replace/serialize/parallel/debounce:Ns")

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
        validate_service(target_dir)
    elif is_ext:
        validate_extension(target_dir)
    else:
        log_fail("目标目录下既无 service.yaml 也无 meta.yaml")
        sys.exit(1)

    run_supd_validate(target_dir)


if __name__ == "__main__":
    main()
