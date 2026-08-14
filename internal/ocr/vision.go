package ocr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Result struct {
	Text  string `json:"text"`
	Lines []Line `json:"lines"`
}

type Line struct {
	Text       string  `json:"text"`
	Confidence float32 `json:"confidence"`
}

type Vision struct {
	scriptPath string
	binaryPath string
	timeout    time.Duration
}

func NewVision(binaryPath, scriptPath string, timeout time.Duration) *Vision {
	return &Vision{binaryPath: binaryPath, scriptPath: scriptPath, timeout: timeout}
}

func (v *Vision) Extract(ctx context.Context, imagePath string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	cmd := v.command(ctx, imagePath)
	output, err := cmd.Output()
	if err != nil {
		return Result{}, err
	}
	var result Result
	if err := json.Unmarshal(output, &result); err != nil {
		return Result{}, err
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.Text == "" {
		return Result{}, errors.New("未识别到文字")
	}
	return result, nil
}

func (v *Vision) command(ctx context.Context, imagePath string) *exec.Cmd {
	if v.binaryPath != "" {
		if _, err := os.Stat(v.binaryPath); err == nil {
			return exec.CommandContext(ctx, v.binaryPath, imagePath)
		}
	}
	return exec.CommandContext(ctx, "swift", v.scriptPath, imagePath)
}
