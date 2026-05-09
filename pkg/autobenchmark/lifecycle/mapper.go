/*
Copyright 2026 The RBG Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lifecycle

import "fmt"

// ParamMapping describes how an abstract parameter maps to a CLI flag.
type ParamMapping struct {
	Flag string // CLI flag name (e.g., "--mem-fraction-static")
}

// ParamMapper maps abstract parameter names to backend-specific CLI flags.
type ParamMapper struct {
	backend  string
	mappings map[string]ParamMapping
}

// sglangMappings maps abstract param names to SGLang CLI flags.
var sglangMappings = map[string]ParamMapping{
	// Memory
	"gpuMemoryUtilization": {Flag: "--mem-fraction-static"},
	"kvCacheDtype":         {Flag: "--kv-cache-dtype"},

	// Batching / Scheduling
	"maxNumSeqs":           {Flag: "--max-running-requests"},
	"maxQueuedRequests":    {Flag: "--max-queued-requests"},
	"chunkedPrefillSize":   {Flag: "--chunked-prefill-size"},
	"maxPrefillTokens":     {Flag: "--max-prefill-tokens"},
	"schedulePolicy":       {Flag: "--schedule-policy"},
	"scheduleConservative": {Flag: "--schedule-conservativeness"},

	// Parallelism
	"tensorParallelSize":   {Flag: "--tensor-parallel-size"},
	"dataParallelSize":     {Flag: "--data-parallel-size"},
	"expertParallelSize":   {Flag: "--expert-parallel-size"},
	"pipelineParallelSize": {Flag: "--pipeline-parallel-size"},

	// Model
	"contextLength": {Flag: "--context-length"},

	// Attention
	"prefillAttentionBackend": {Flag: "--prefill-attention-backend"},
	"decodeAttentionBackend":  {Flag: "--decode-attention-backend"},

	// Optimization
	"enableCudaGraph":    {Flag: "--enable-cuda-graph"},
	"enableTorchCompile": {Flag: "--enable-torch-compile"},

	// Speculative decoding
	"speculativeAlgorithm": {Flag: "--speculative-algorithm"},
	"draftModelPath":       {Flag: "--speculative-draft-model-path"},
	"speculativeNumSteps":  {Flag: "--speculative-num-steps"},
	"speculativeEagleTopk": {Flag: "--speculative-eagle-topk"},
	"numSpeculativeTokens": {Flag: "--speculative-num-draft-tokens"},

	// Quantization / dtype
	"quantization": {Flag: "--quantization"},
	"dtype":        {Flag: "--dtype"},
}

// vllmMappings maps abstract param names to vLLM CLI flags.
var vllmMappings = map[string]ParamMapping{
	// Memory
	"gpuMemoryUtilization": {Flag: "--gpu-memory-utilization"},
	"kvCacheDtype":         {Flag: "--kv-cache-dtype"},

	// Batching
	"maxNumSeqs":           {Flag: "--max-num-seqs"},
	"maxNumBatchedTokens":  {Flag: "--max-num-batched-tokens"},
	"enableChunkedPrefill": {Flag: "--enable-chunked-prefill"},

	// Parallelism
	"tensorParallelSize":   {Flag: "--tensor-parallel-size"},
	"dataParallelSize":     {Flag: "--data-parallel-size"},
	"pipelineParallelSize": {Flag: "--pipeline-parallel-size"},

	// Model
	"contextLength": {Flag: "--max-model-len"},

	// Attention
	"attentionBackend": {Flag: "--attention-backend"},

	// Optimization
	"enforceEager":        {Flag: "--enforce-eager"},
	"enablePrefixCaching": {Flag: "--enable-prefix-caching"},

	// Speculative decoding
	"draftModelPath":       {Flag: "--speculative-model"},
	"numSpeculativeTokens": {Flag: "--num-speculative-tokens"},

	// Quantization / dtype
	"quantization": {Flag: "--quantization"},
	"dtype":        {Flag: "--dtype"},
}

// NewParamMapper creates a ParamMapper for the given backend.
func NewParamMapper(backend string) (*ParamMapper, error) {
	var mappings map[string]ParamMapping
	switch backend {
	case "sglang":
		mappings = sglangMappings
	case "vllm":
		mappings = vllmMappings
	default:
		return nil, fmt.Errorf("unsupported backend: %q", backend)
	}
	return &ParamMapper{backend: backend, mappings: mappings}, nil
}

// MapParam returns the CLI flag for a given abstract parameter name.
func (pm *ParamMapper) MapParam(paramName string) (string, error) {
	m, ok := pm.mappings[paramName]
	if !ok {
		return "", fmt.Errorf("unknown parameter %q for backend %q", paramName, pm.backend)
	}
	return m.Flag, nil
}

// Backend returns the backend name.
func (pm *ParamMapper) Backend() string { return pm.backend }

// OverlayArgs takes existing command args and overlays the given params
// using the param mapper. It replaces existing flag values or appends new flags.
func (pm *ParamMapper) OverlayArgs(args []string, params map[string]interface{}) ([]string, error) {
	// Make a copy to avoid mutating the original
	result := make([]string, len(args))
	copy(result, args)

	for paramName, value := range params {
		flag, err := pm.MapParam(paramName)
		if err != nil {
			return nil, err
		}
		strVal := fmt.Sprintf("%v", value)
		result = replaceOrAppendFlag(result, flag, strVal)
	}
	return result, nil
}

// replaceOrAppendFlag replaces an existing flag's value or appends a new flag-value pair.
// Handles both "--flag value" and "--flag=value" formats.
func replaceOrAppendFlag(args []string, flag, value string) []string {
	for i := 0; i < len(args); i++ {
		// Check "--flag value" format
		if args[i] == flag && i+1 < len(args) {
			args[i+1] = value
			return args
		}
		// Check "--flag=value" format
		if len(args[i]) > len(flag) && args[i][:len(flag)+1] == flag+"=" {
			args[i] = flag + "=" + value
			return args
		}
	}
	// Flag not found, append
	return append(args, flag, value)
}
