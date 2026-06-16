package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/samber/lo"
)

// 官方价格来源常量
const (
	sourceOfficial  = "official"   // basellm.github.io 官方倍率预设
	sourceModelsDev = "modelsdev"  // models.dev 多供应商价格聚合

	officialBaseURL      = "https://basellm.github.io"
	officialEndpointPath = "/api/ratio_config"
	modelsDevBaseURL     = "https://models.dev"
	modelsDevPath        = "/api.json"

	modelsDevInputCostRatioBase = 1000.0
	floatEpsilonSync            = 1e-9

	syncHTTPTimeout       = 30 * time.Second
	maxConcurrentSources  = 2
	maxRatioConfigBytesSync = 10 << 20 // 10MB
	autoSyncCheckInterval = 10 * time.Minute
)

var (
	lastAutoSyncTime  time.Time
	lastAutoSyncCount int
	autoSyncLock      sync.Mutex
)

// ratioData 合并后的倍率数据，结构与 ratio_setting 暴露的一致。
type ratioData struct {
	ModelRatio            map[string]float64
	CompletionRatio       map[string]float64
	CacheRatio            map[string]float64
	ImageRatio            map[string]float64
	AudioRatio            map[string]float64
	AudioCompletionRatio  map[string]float64
}

func newRatioData() *ratioData {
	return &ratioData{
		ModelRatio:           make(map[string]float64),
		CompletionRatio:      make(map[string]float64),
		CacheRatio:           make(map[string]float64),
		ImageRatio:           make(map[string]float64),
		AudioRatio:           make(map[string]float64),
		AudioCompletionRatio: make(map[string]float64),
	}
}

// mergeFrom 将来源数据合并到目标，仅填充目标中缺失的键（来源优先级低的兜底）。
func (r *ratioData) mergeFrom(src map[string]any, overwrite bool) {
	mergeMap := func(target map[string]float64, key string) {
		raw, ok := src[key]
		if !ok {
			return
		}
		m := valueMapSync(raw)
		for k, v := range m {
			f, ok := asFloat64Sync(v)
			if !ok {
				continue
			}
			if overwrite {
				target[k] = f
			} else if _, exists := target[k]; !exists {
				target[k] = f
			}
		}
	}
	mergeMap(r.ModelRatio, "model_ratio")
	mergeMap(r.CompletionRatio, "completion_ratio")
	mergeMap(r.CacheRatio, "cache_ratio")
	mergeMap(r.ImageRatio, "image_ratio")
	mergeMap(r.AudioRatio, "audio_ratio")
	mergeMap(r.AudioCompletionRatio, "audio_completion_ratio")
}

func valueMapSync(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]float64:
		return lo.MapValues(typed, func(value float64, _ string) any { return value })
	default:
		return nil
	}
}

func asFloat64Sync(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func roundRatioValueSync(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}

func isModelsDevEndpointSync(rawURL string) bool {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if strings.ToLower(parsedURL.Hostname()) != "models.dev" {
		return false
	}
	path := strings.TrimSuffix(parsedURL.Path, "/")
	if path == "" {
		path = "/"
	}
	return path == modelsDevPath
}

// fetchURLWithRetry 拉取指定 URL 内容，带简单重试和 github.io IPv4 优先。
func fetchURLWithRetry(ctx context.Context, fullURL string) ([]byte, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if strings.HasSuffix(host, "github.io") {
			if conn, err := dialer.DialContext(ctx, "tcp4", addr); err == nil {
				return conn, nil
			}
			return dialer.DialContext(ctx, "tcp6", addr)
		}
		return dialer.DialContext(ctx, network, addr)
	}
	client := &http.Client{Transport: transport}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %s", resp.Status)
			time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
			continue
		}
		limited := io.LimitReader(resp.Body, maxRatioConfigBytesSync)
		body, err := io.ReadAll(limited)
		if err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, lastErr
}

// fetchOfficialRatio 从 basellm.github.io/api/ratio_config 拉取官方倍率预设。
// 该接口返回 new-api 标准的 type1 格式：{ success, data: { model_ratio: {...}, ... } }
func fetchOfficialRatio(ctx context.Context) (map[string]any, error) {
	fullURL := officialBaseURL + officialEndpointPath
	body, err := fetchURLWithRetry(ctx, fullURL)
	if err != nil {
		return nil, fmt.Errorf("fetch official ratio failed: %w", err)
	}
	var resp struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := common.DecodeJson(bytes.NewReader(body), &resp); err != nil {
		return nil, fmt.Errorf("decode official ratio failed: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("official ratio not success: %s", resp.Message)
	}
	var data map[string]any
	if err := common.Unmarshal(resp.Data, &data); err != nil {
		// 尝试作为 pricing 列表（type2）解析
		return parsePricingListToMap(resp.Data)
	}
	// 校验是否包含至少一个已知字段
	knownFields := []string{"model_ratio", "completion_ratio", "cache_ratio", "model_price", "image_ratio", "audio_ratio"}
	hasField := false
	for _, f := range knownFields {
		if _, ok := data[f]; ok {
			hasField = true
			break
		}
	}
	if !hasField {
		return nil, fmt.Errorf("official ratio data missing known fields")
	}
	return data, nil
}

// parsePricingListToMap 将 type2（/api/pricing 返回的 []Pricing 列表）转为 type1 map 格式。
func parsePricingListToMap(raw json.RawMessage) (map[string]any, error) {
	var items []struct {
		ModelName            string   `json:"model_name"`
		QuotaType            int      `json:"quota_type"`
		ModelRatio           float64  `json:"model_ratio"`
		CompletionRatio      float64  `json:"completion_ratio"`
		CacheRatio           *float64 `json:"cache_ratio"`
		ImageRatio           *float64 `json:"image_ratio"`
		AudioRatio           *float64 `json:"audio_ratio"`
		AudioCompletionRatio *float64 `json:"audio_completion_ratio"`
	}
	if err := common.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	result := make(map[string]any)
	modelRatio := make(map[string]any)
	completionRatio := make(map[string]any)
	cacheRatio := make(map[string]any)
	imageRatio := make(map[string]any)
	audioRatio := make(map[string]any)
	audioCompletionRatio := make(map[string]any)
	for _, item := range items {
		if item.ModelName == "" || item.QuotaType == 1 {
			continue
		}
		modelRatio[item.ModelName] = item.ModelRatio
		completionRatio[item.ModelName] = item.CompletionRatio
		if item.CacheRatio != nil {
			cacheRatio[item.ModelName] = *item.CacheRatio
		}
		if item.ImageRatio != nil {
			imageRatio[item.ModelName] = *item.ImageRatio
		}
		if item.AudioRatio != nil {
			audioRatio[item.ModelName] = *item.AudioRatio
		}
		if item.AudioCompletionRatio != nil {
			audioCompletionRatio[item.ModelName] = *item.AudioCompletionRatio
		}
	}
	if len(modelRatio) > 0 {
		result["model_ratio"] = modelRatio
	}
	if len(completionRatio) > 0 {
		result["completion_ratio"] = completionRatio
	}
	if len(cacheRatio) > 0 {
		result["cache_ratio"] = cacheRatio
	}
	if len(imageRatio) > 0 {
		result["image_ratio"] = imageRatio
	}
	if len(audioRatio) > 0 {
		result["audio_ratio"] = audioRatio
	}
	if len(audioCompletionRatio) > 0 {
		result["audio_completion_ratio"] = audioCompletionRatio
	}
	return result, nil
}

// fetchModelsDevRatio 从 models.dev/api.json 拉取并转换为本地倍率格式。
func fetchModelsDevRatio(ctx context.Context) (map[string]any, error) {
	fullURL := modelsDevBaseURL + modelsDevPath
	body, err := fetchURLWithRetry(ctx, fullURL)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev failed: %w", err)
	}
	return convertModelsDevToMap(bytes.NewReader(body))
}

type modelsDevProviderSync struct {
	Models map[string]modelsDevModelSync `json:"models"`
}

type modelsDevModelSync struct {
	Cost modelsDevCostSync `json:"cost"`
}

type modelsDevCostSync struct {
	Input     *float64 `json:"input"`
	Output    *float64 `json:"output"`
	CacheRead *float64 `json:"cache_read"`
}

type modelsDevCandidateSync struct {
	Provider  string
	Input     float64
	Output    *float64
	CacheRead *float64
}

func cloneFloatPtrSync(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func isValidNonNegativeCostSync(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	return v >= 0
}

func buildCandidateSync(provider string, cost modelsDevCostSync) (modelsDevCandidateSync, bool) {
	if cost.Input == nil {
		return modelsDevCandidateSync{}, false
	}
	input := *cost.Input
	if !isValidNonNegativeCostSync(input) {
		return modelsDevCandidateSync{}, false
	}
	var output *float64
	if cost.Output != nil {
		if !isValidNonNegativeCostSync(*cost.Output) {
			return modelsDevCandidateSync{}, false
		}
		output = cloneFloatPtrSync(cost.Output)
	}
	if input == 0 && output != nil && *output > 0 {
		return modelsDevCandidateSync{}, false
	}
	var cacheRead *float64
	if cost.CacheRead != nil && isValidNonNegativeCostSync(*cost.CacheRead) {
		cacheRead = cloneFloatPtrSync(cost.CacheRead)
	}
	return modelsDevCandidateSync{
		Provider:  provider,
		Input:     input,
		Output:    output,
		CacheRead: cacheRead,
	}, true
}

func shouldReplaceCandidateSync(current, next modelsDevCandidateSync) bool {
	currentNonZero := current.Input > 0
	nextNonZero := next.Input > 0
	if currentNonZero != nextNonZero {
		return nextNonZero
	}
	if nextNonZero && math.Abs(next.Input-current.Input) > floatEpsilonSync {
		return next.Input < current.Input
	}
	return next.Provider < current.Provider
}

// convertModelsDevToMap 解析 models.dev /api.json 并转换为本地 ratio map。
// models.dev 价格为 USD/1M tokens：
//
//	model_ratio = input_cost_per_1M / 2
//	completion_ratio = output_cost / input_cost
//	cache_ratio = cache_read_cost / input_cost
func convertModelsDevToMap(reader io.Reader) (map[string]any, error) {
	var upstreamData map[string]modelsDevProviderSync
	if err := common.DecodeJson(reader, &upstreamData); err != nil {
		return nil, fmt.Errorf("failed to decode models.dev response: %w", err)
	}
	if len(upstreamData) == 0 {
		return nil, fmt.Errorf("empty models.dev response")
	}

	providers := make([]string, 0, len(upstreamData))
	for provider := range upstreamData {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	selected := make(map[string]modelsDevCandidateSync)
	for _, provider := range providers {
		providerData := upstreamData[provider]
		if len(providerData.Models) == 0 {
			continue
		}
		modelNames := make([]string, 0, len(providerData.Models))
		for modelName := range providerData.Models {
			modelNames = append(modelNames, modelName)
		}
		sort.Strings(modelNames)
		for _, modelName := range modelNames {
			candidate, ok := buildCandidateSync(provider, providerData.Models[modelName].Cost)
			if !ok {
				continue
			}
			current, exists := selected[modelName]
			if !exists || shouldReplaceCandidateSync(current, candidate) {
				selected[modelName] = candidate
			}
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no valid models.dev pricing entries found")
	}

	modelRatioMap := make(map[string]any)
	completionRatioMap := make(map[string]any)
	cacheRatioMap := make(map[string]any)
	for modelName, candidate := range selected {
		if candidate.Input == 0 {
			modelRatioMap[modelName] = 0.0
			continue
		}
		modelRatio := candidate.Input * float64(ratio_setting.USD) / modelsDevInputCostRatioBase
		modelRatioMap[modelName] = roundRatioValueSync(modelRatio)
		if candidate.Output != nil {
			completionRatio := *candidate.Output / candidate.Input
			completionRatioMap[modelName] = roundRatioValueSync(completionRatio)
		}
		if candidate.CacheRead != nil {
			cacheRatio := *candidate.CacheRead / candidate.Input
			cacheRatioMap[modelName] = roundRatioValueSync(cacheRatio)
		}
	}

	converted := make(map[string]any)
	if len(modelRatioMap) > 0 {
		converted["model_ratio"] = modelRatioMap
	}
	if len(completionRatioMap) > 0 {
		converted["completion_ratio"] = completionRatioMap
	}
	if len(cacheRatioMap) > 0 {
		converted["cache_ratio"] = cacheRatioMap
	}
	return converted, nil
}

// SyncOfficialRatios 执行一次官方价格同步：按配置的来源优先级拉取并应用到本地倍率表。
// 返回同步的模型数量。注意：不触碰 model_price（按次计费）和 group_ratio（分组倍率）。
func SyncOfficialRatios() (int, error) {
	autoSyncLock.Lock()
	defer autoSyncLock.Unlock()

	sources := parseSources(operation_setting.OfficialRatioSyncSources)
	if len(sources) == 0 {
		sources = []string{sourceOfficial, sourceModelsDev}
	}

	ctx, cancel := context.WithTimeout(context.Background(), syncHTTPTimeout*2)
	defer cancel()

	merged := newRatioData()

	// 按优先级顺序拉取：高优先级来源 overwrite=true（覆盖），低优先级兜底 overwrite=false
	for _, src := range sources {
		var (
			data map[string]any
			err  error
		)
		switch src {
		case sourceOfficial:
			data, err = fetchOfficialRatio(ctx)
		case sourceModelsDev:
			data, err = fetchModelsDevRatio(ctx)
		default:
			continue
		}
		if err != nil {
			common.SysLog(fmt.Sprintf("[官方价格同步] 来源 %s 拉取失败: %v", src, err))
			continue
		}
		// 第一个来源覆盖，后续仅填充缺失
		isFirst := len(merged.ModelRatio) == 0 && len(merged.CacheRatio) == 0
		merged.mergeFrom(data, isFirst)
		common.SysLog(fmt.Sprintf("[官方价格同步] 来源 %s 拉取成功", src))
	}

	if len(merged.ModelRatio) == 0 {
		return 0, fmt.Errorf("所有价格来源均无有效数据")
	}

	// 应用到本地：合并而非覆盖，保留用户已有的自定义倍率
	if err := applyMergedRatios(merged); err != nil {
		return 0, err
	}

	count := len(merged.ModelRatio)
	lastAutoSyncTime = time.Now()
	lastAutoSyncCount = count

	// 记录同步时间到 option，供前端展示
	operation_setting.OfficialRatioLastSyncTime = lastAutoSyncTime.Unix()
	operation_setting.OfficialRatioLastSyncCount = count
	_ = model.UpdateOption("OfficialRatioLastSyncTime", strconv.FormatInt(lastAutoSyncTime.Unix(), 10))
	_ = model.UpdateOption("OfficialRatioLastSyncCount", strconv.Itoa(count))

	common.SysLog(fmt.Sprintf("[官方价格同步] 同步完成，共 %d 个模型", count))
	return count, nil
}

func parseSources(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == sourceOfficial || p == sourceModelsDev {
			result = append(result, p)
		}
	}
	return result
}

// applyMergedRatios 将合并后的倍率数据应用到本地配置。
// 采用合并策略：对每个模型，仅当本地未配置时才写入（避免覆盖用户自定义）。
func applyMergedRatios(merged *ratioData) error {
	// 获取本地现有数据
	localModelRatio := ratio_setting.GetModelRatioCopy()
	localCompletionRatio := ratio_setting.GetCompletionRatioCopy()
	localCacheRatio := ratio_setting.GetCacheRatioCopy()
	localImageRatio := ratio_setting.GetImageRatioCopy()
	localAudioRatio := ratio_setting.GetAudioRatioCopy()
	localAudioCompletionRatio := ratio_setting.GetAudioCompletionRatioCopy()

	// 合并：远程值填充本地缺失的键
	changed := false
	for k, v := range merged.ModelRatio {
		if _, exists := localModelRatio[k]; !exists {
			localModelRatio[k] = v
			changed = true
		}
	}
	for k, v := range merged.CompletionRatio {
		if _, exists := localCompletionRatio[k]; !exists {
			localCompletionRatio[k] = v
			changed = true
		}
	}
	for k, v := range merged.CacheRatio {
		if _, exists := localCacheRatio[k]; !exists {
			localCacheRatio[k] = v
			changed = true
		}
	}
	for k, v := range merged.ImageRatio {
		if _, exists := localImageRatio[k]; !exists {
			localImageRatio[k] = v
			changed = true
		}
	}
	for k, v := range merged.AudioRatio {
		if _, exists := localAudioRatio[k]; !exists {
			localAudioRatio[k] = v
			changed = true
		}
	}
	for k, v := range merged.AudioCompletionRatio {
		if _, exists := localAudioCompletionRatio[k]; !exists {
			localAudioCompletionRatio[k] = v
			changed = true
		}
	}

	if !changed {
		common.SysLog("[官方价格同步] 本地已包含所有远程价格，无需更新")
		return nil
	}

	// 序列化并持久化
	if jsonStr, err := marshalRatioMap(localModelRatio); err == nil {
		if err := model.UpdateOption("ModelRatio", jsonStr); err != nil {
			return fmt.Errorf("persist ModelRatio failed: %w", err)
		}
		_ = ratio_setting.UpdateModelRatioByJSONString(jsonStr)
	}
	if jsonStr, err := marshalRatioMap(localCompletionRatio); err == nil {
		if err := model.UpdateOption("CompletionRatio", jsonStr); err != nil {
			return fmt.Errorf("persist CompletionRatio failed: %w", err)
		}
		_ = ratio_setting.UpdateCompletionRatioByJSONString(jsonStr)
	}
	if len(localCacheRatio) > 0 {
		if jsonStr, err := marshalRatioMap(localCacheRatio); err == nil {
			_ = model.UpdateOption("CacheRatio", jsonStr)
			_ = ratio_setting.UpdateCacheRatioByJSONString(jsonStr)
		}
	}
	if len(localImageRatio) > 0 {
		if jsonStr, err := marshalRatioMap(localImageRatio); err == nil {
			_ = model.UpdateOption("ImageRatio", jsonStr)
			_ = ratio_setting.UpdateImageRatioByJSONString(jsonStr)
		}
	}
	if len(localAudioRatio) > 0 {
		if jsonStr, err := marshalRatioMap(localAudioRatio); err == nil {
			_ = model.UpdateOption("AudioRatio", jsonStr)
			_ = ratio_setting.UpdateAudioRatioByJSONString(jsonStr)
		}
	}
	if len(localAudioCompletionRatio) > 0 {
		if jsonStr, err := marshalRatioMap(localAudioCompletionRatio); err == nil {
			_ = model.UpdateOption("AudioCompletionRatio", jsonStr)
			_ = ratio_setting.UpdateAudioCompletionRatioByJSONString(jsonStr)
		}
	}

	return nil
}

func marshalRatioMap(m map[string]float64) (string, error) {
	jsonBytes, err := common.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

// GetLastAutoSyncInfo 返回上次自动同步的时间和模型数，供前端展示。
func GetLastAutoSyncInfo() (time.Time, int) {
	autoSyncLock.Lock()
	defer autoSyncLock.Unlock()
	return lastAutoSyncTime, lastAutoSyncCount
}

// StartOfficialRatioAutoSyncTask 启动后台定时任务，按配置间隔自动同步官方价格。
// 在 main.go 中与其他 Start* 任务一起注册。
func StartOfficialRatioAutoSyncTask() {
	go func() {
		// 启动时等待 30 秒，让其他初始化完成
		time.Sleep(30 * time.Second)

		ticker := time.NewTicker(autoSyncCheckInterval)
		defer ticker.Stop()

		for range ticker.C {
			if !operation_setting.AutoSyncOfficialRatioEnabled {
				continue
			}
			intervalHours := operation_setting.OfficialRatioSyncIntervalHours
			if intervalHours <= 0 {
				intervalHours = 24
			}
			interval := time.Duration(intervalHours) * time.Hour
			if !lastAutoSyncTime.IsZero() && time.Since(lastAutoSyncTime) < interval {
				continue
			}
			// 执行同步（同步内部有锁，这里不阻塞 ticker 过久）
			go func() {
				defer func() {
					if r := recover(); r != nil {
						common.SysError(fmt.Sprintf("[官方价格同步] panic: %v", r))
					}
				}()
				_, err := SyncOfficialRatios()
				if err != nil {
					common.SysError("[官方价格同步] 同步失败: " + err.Error())
				}
			}()
		}
	}()
}
