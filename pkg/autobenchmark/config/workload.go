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

package config

import (
	"fmt"
	"regexp"
	"strconv"
)

// WorkloadType enumerates the supported workload distribution types.
type WorkloadType string

const (
	WorkloadFixed   WorkloadType = "fixed"
	WorkloadNormal  WorkloadType = "normal"
	WorkloadUniform WorkloadType = "uniform"
	WorkloadDataset WorkloadType = "dataset"
)

// Workload represents a parsed workload string.
type Workload struct {
	Type         WorkloadType
	InputTokens  int // fixed: exact input token count
	OutputTokens int // fixed: exact output token count
	InputMean    int // normal: input mean
	InputStdDev  int // normal: input standard deviation
	OutputMean   int // normal: output mean
	OutputStdDev int // normal: output standard deviation
	InputMin     int // uniform: input minimum
	InputMax     int // uniform: input maximum
	OutputMin    int // uniform: output minimum
	OutputMax    int // uniform: output maximum
}

var (
	fixedRe   = regexp.MustCompile(`^fixed\((\d+),(\d+)\)$`)
	normalRe  = regexp.MustCompile(`^normal\((\d+),(\d+)/(\d+),(\d+)\)$`)
	uniformRe = regexp.MustCompile(`^uniform\((\d+),(\d+)/(\d+),(\d+)\)$`)
	datasetRe = regexp.MustCompile(`^dataset$`)
)

// ParseWorkload parses a workload string into a Workload struct.
// Supported formats:
//   - fixed(input,output)           e.g. fixed(100,1000)
//   - normal(μ_in,σ_in/μ_out,σ_out) e.g. normal(480,240/300,150)
//   - uniform(min_in,max_in/min_out,max_out) e.g. uniform(100,500/200,800)
//   - dataset
func ParseWorkload(s string) (*Workload, error) {
	if datasetRe.MatchString(s) {
		return &Workload{Type: WorkloadDataset}, nil
	}

	if m := fixedRe.FindStringSubmatch(s); m != nil {
		input, _ := strconv.Atoi(m[1])
		output, _ := strconv.Atoi(m[2])
		return &Workload{
			Type:         WorkloadFixed,
			InputTokens:  input,
			OutputTokens: output,
		}, nil
	}

	if m := normalRe.FindStringSubmatch(s); m != nil {
		inputMean, _ := strconv.Atoi(m[1])
		inputStd, _ := strconv.Atoi(m[2])
		outputMean, _ := strconv.Atoi(m[3])
		outputStd, _ := strconv.Atoi(m[4])
		return &Workload{
			Type:         WorkloadNormal,
			InputMean:    inputMean,
			InputStdDev:  inputStd,
			OutputMean:   outputMean,
			OutputStdDev: outputStd,
		}, nil
	}

	if m := uniformRe.FindStringSubmatch(s); m != nil {
		inputMin, _ := strconv.Atoi(m[1])
		inputMax, _ := strconv.Atoi(m[2])
		outputMin, _ := strconv.Atoi(m[3])
		outputMax, _ := strconv.Atoi(m[4])
		return &Workload{
			Type:      WorkloadUniform,
			InputMin:  inputMin,
			InputMax:  inputMax,
			OutputMin: outputMin,
			OutputMax: outputMax,
		}, nil
	}

	return nil, fmt.Errorf("invalid workload string %q: expected one of fixed(in,out), normal(μ_in,σ_in/μ_out,σ_out), uniform(min_in,max_in/min_out,max_out), dataset", s)
}

// ValidateWorkload validates that the string is a parseable workload.
func ValidateWorkload(s string) error {
	_, err := ParseWorkload(s)
	return err
}
