#!/usr/bin/env bash
# speed-go 编译脚本
# 用法: ./build.sh [选项] [目标]
#   目标: all(默认) / amd64 / mt7981
#   选项: --upx   使用 upx 压缩产物(默认不压缩)
#
# 示例:
#   ./build.sh                    编译全部目标,不压缩
#   ./build.sh mt7981             只编译 MT7981(ARM64),不压缩
#   ./build.sh --upx              编译全部目标,upx 压缩
#   ./build.sh --upx amd64        只编译 linux 64 位,upx 压缩
set -euo pipefail

# 版本号: 优先取环境变量 GOSPEED_VERSION(CI 传入 git tag), 否则用日期
VERSION="${GOSPEED_VERSION:-$(date +%Y%m%d)}"
LDFLAGS="-s -w -X main.version=${VERSION}"
# 产物目录: 可用环境变量 GOSPEED_OUTDIR 覆盖(CI 用于区分压缩/未压缩产物)
OUTDIR="${GOSPEED_OUTDIR:-bin}"
USE_UPX=0

# 目标列表: 名称|GOARCH|输出文件
TARGETS=(
    "amd64|amd64|speed-go-linux-amd64"
    "mt7981|arm64|speed-go-linux-mt7981-arm64"
)

# 解析参数:--upx 为选项,其余为目标名
FILTER="all"
for arg in "$@"; do
    case "${arg}" in
        --upx) USE_UPX=1 ;;
        *)     FILTER="${arg}" ;;
    esac
done

if [[ "${USE_UPX}" -eq 1 ]] && ! command -v upx >/dev/null 2>&1; then
    echo "错误: 指定了 --upx 但未安装 upx" >&2
    exit 1
fi

mkdir -p "${OUTDIR}"

build() {
    local name="$1" goarch="$2" outfile="$3"

    echo ">>> 编译 ${name} (linux/${goarch}) ..."
    CGO_ENABLED=0 GOARCH="${goarch}" \
        go build -trimpath -ldflags="${LDFLAGS}" -o "${OUTDIR}/${outfile}" .

    if [[ "${USE_UPX}" -eq 1 ]]; then
        # 单个架构压缩失败不影响其他目标
        upx --best -q "${OUTDIR}/${outfile}" >/dev/null 2>&1 || \
            echo "    upx 压缩失败(该架构可能不支持)"
    fi

    printf "    %-32s %s\n" "${OUTDIR}/${outfile}" "$(du -h "${OUTDIR}/${outfile}" | cut -f1)"
}

built=0
for t in "${TARGETS[@]}"; do
    IFS='|' read -r name goarch outfile <<< "${t}"
    if [[ "${FILTER}" == "all" || "${FILTER}" == "${name}" ]]; then
        build "${name}" "${goarch}" "${outfile}"
        built=1
    fi
done

if [[ "${built}" -eq 0 ]]; then
    echo "错误: 未知目标 '${FILTER}',可用: amd64 / mt7981 / all,选项: --upx" >&2
    exit 1
fi

echo ">>> 完成,产物在 ${OUTDIR}/ 目录$( [[ "${USE_UPX}" -eq 1 ]] && echo '(已 upx 压缩)' )"
