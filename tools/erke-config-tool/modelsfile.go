//go:build windows

package main

// modelsfile.go — models.json 的读写与格式兼容层。
//
// 背景（2026-09-17 定位）：WorkBuddy 主进程每次启动都会跑硬件白名单校验，校验不通过时
// 调用 purgeLocalModelsOnGateFail() 清理本地模型；该函数**只认裸数组** [{...}]，
// 遇到对象包裹格式 {"models":[...]} 会判定为空并把整个文件原子重写为 []——配置文件就没了。
// daemon / CoreServices / CLI 读取侧都兼容两种格式，只有这一个清理函数不兼容。
//
// 因此本工具统一按**裸数组**落盘（顶层必须是 `[`），并且在写入前把既有的对象包裹格式
// 归一化过来；同时保留用户自己的其它模型条目（按 id 合并，不整文件覆盖）。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// workbuddyDir 返回目标客户端的数据目录（~/.workbuddy 或 ~/.codebuddy）
func workbuddyDir(product string) (string, error) {
	dirName := ".workbuddy"
	if product == "codebuddy" {
		dirName = ".codebuddy"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func modelsFilePath(product string) (string, error) {
	dir, err := workbuddyDir(product)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.json"), nil
}

// loadModelsAnyFormat 读取 models.json，兼容两种落盘格式。
// 返回 (模型列表, 原始格式 "array"/"object"/"empty")。
func loadModelsAnyFormat(target string) ([]usageModel, string, error) {
	raw, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "empty", nil
		}
		return nil, "", err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, "empty", nil
	}
	// 1) 裸数组（WorkBuddy UI、本工具、daemon 保存的格式 —— 安全格式）
	var arr []usageModel
	if err := json.Unmarshal(trimmed, &arr); err == nil {
		return arr, "array", nil
	}
	// 2) 对象包裹（旧手写配置 / CLI cbc 路径写的格式 —— 会被 purge 清空）
	var wrap struct {
		Models []usageModel `json:"models"`
	}
	if err := json.Unmarshal(trimmed, &wrap); err == nil && wrap.Models != nil {
		return wrap.Models, "object", nil
	}
	return nil, "", fmt.Errorf("无法解析 %s（既不是裸数组也不是 {\"models\":[...]}）", filepath.Base(target))
}

// mergeModelsByID 按 id 合并：incoming 覆盖同 id 条目，existing 中其它条目保留。
// 返回合并结果与"实际新增/更新"的条数。
func mergeModelsByID(existing, incoming []usageModel) ([]usageModel, int) {
	out := make([]usageModel, 0, len(existing)+len(incoming))
	idx := make(map[string]int, len(existing))
	for _, m := range existing {
		if strings.TrimSpace(m.Id) == "" {
			continue
		}
		idx[m.Id] = len(out)
		out = append(out, m)
	}
	changed := 0
	for _, m := range incoming {
		if i, ok := idx[m.Id]; ok {
			if !sameModel(out[i], m) {
				out[i] = m
				changed++
			}
			continue
		}
		idx[m.Id] = len(out)
		out = append(out, m)
		changed++
	}
	return out, changed
}

func sameModel(a, b usageModel) bool {
	return a.Id == b.Id && a.Name == b.Name && a.Provider == b.Provider && a.URL == b.URL &&
		a.APIKey == b.APIKey && a.MaxInputTokens == b.MaxInputTokens && a.MaxOutputTokens == b.MaxOutputTokens &&
		a.SupportsToolCall == b.SupportsToolCall && a.SupportsImages == b.SupportsImages &&
		a.SupportsReasoning == b.SupportsReasoning
}

// writeModelsBareArray 以**裸数组**格式原子写入（临时文件 + rename），写完自检首字符为 `[`。
func writeModelsBareArray(target string, models []usageModel) error {
	if models == nil {
		models = []usageModel{}
	}
	out, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	temp := target + ".tmp"
	if err := os.WriteFile(temp, out, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temp, target); err != nil {
		_ = os.Remove(temp)
		return err
	}
	// 自检：顶层必须是 `[`，且能按数组解析回来（防止将来有人又把格式改回对象包裹）
	check, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if first := bytes.TrimSpace(check); len(first) == 0 || first[0] != '[' {
		return fmt.Errorf("写入后自检失败：文件顶层不是裸数组（会被 WorkBuddy 硬件门限清理）")
	}
	var back []usageModel
	if err := json.Unmarshal(check, &back); err != nil {
		return fmt.Errorf("写入后自检失败：%w", err)
	}
	if len(back) != len(models) {
		return fmt.Errorf("写入后自检失败：条数不符（期望 %d，实际 %d）", len(models), len(back))
	}
	return nil
}

// writeModelsFileMerged 写入配置：读取既有内容（兼容两种格式）→ 按 id 合并 → 裸数组原子写入。
// 返回 (文件路径, 合并后总条数, 新写入/更新的条数)
// 说明：写入是「临时文件 + rename」的原子替换且写完自检，不再额外留 .bak 备份文件。
func writeModelsFileMerged(product string, cfg *usageConfig) (string, int, int, error) {
	target, err := modelsFilePath(product)
	if err != nil {
		return "", 0, 0, err
	}
	existing, _, err := loadModelsAnyFormat(target)
	if err != nil {
		return "", 0, 0, err
	}
	merged, changed := mergeModelsByID(existing, cfg.Models)
	if err := writeModelsBareArray(target, merged); err != nil {
		return target, 0, 0, err
	}
	return target, len(merged), changed, nil
}

// normalizeModelsFile 把既有 models.json 归一化为裸数组（无需配置码，纯修复）。
// 返回 (路径, 条数, 是否发生了改动)
func normalizeModelsFile(product string) (string, int, bool, error) {
	target, err := modelsFilePath(product)
	if err != nil {
		return "", 0, false, err
	}
	models, format, err := loadModelsAnyFormat(target)
	if err != nil {
		return target, 0, false, err
	}
	if format == "array" || format == "empty" {
		return target, len(models), false, nil
	}
	if err := writeModelsBareArray(target, models); err != nil {
		return target, 0, false, err
	}
	return target, len(models), true, nil
}

// inspectModelsFile 返回 (路径, 格式 array/object/empty, 条数)
func inspectModelsFile(product string) (string, string, int, error) {
	target, err := modelsFilePath(product)
	if err != nil {
		return "", "", 0, err
	}
	models, format, err := loadModelsAnyFormat(target)
	if err != nil {
		return target, "invalid", 0, err
	}
	return target, format, len(models), nil
}
