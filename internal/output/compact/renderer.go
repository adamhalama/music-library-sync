package compact

import (
	"fmt"
	"strings"
)

func RenderTrackLine(name string, stage string, hasThumbnail bool, hasMetadata bool, progressKnown bool, progressPercent float64, alreadyPresent bool, index int, total int) string {
	line := fmt.Sprintf("[track] %s", name)
	if total > 0 {
		line = fmt.Sprintf("[track %d/%d] %s", index, total, name)
	}

	bits := []string{}
	if strings.TrimSpace(stage) != "" {
		bits = append(bits, stage)
	}
	if hasThumbnail {
		bits = append(bits, "thumb:yes")
	}
	if hasMetadata {
		bits = append(bits, "meta:yes")
	}
	if len(bits) > 0 {
		line += " (" + strings.Join(bits, ", ") + ")"
	}
	if progressKnown && !alreadyPresent {
		line += " " + RenderProgress(progressPercent, 16)
	}
	return line
}

func RenderGlobalLine(percent float64, width int, done int, total int) string {
	if total <= 0 {
		return fmt.Sprintf("[overall] %s", RenderProgress(percent, width))
	}
	return fmt.Sprintf("[overall] %s (%d/%d)", RenderProgress(percent, width), done, total)
}

func RenderIdleTrackLine(done int, total int) string {
	if done >= total {
		return fmt.Sprintf("[track] all planned tracks complete (%d/%d)", done, total)
	}
	return fmt.Sprintf("[track] waiting for next track (%d/%d done)", done, total)
}

func RenderProgress(percent float64, width int) string {
	clamped := ClampPercent(percent)
	if width <= 0 {
		width = 16
	}
	filled := int((clamped / 100) * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	return fmt.Sprintf("[%s] %5.1f%%", bar, clamped)
}

func ClampPercent(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}
