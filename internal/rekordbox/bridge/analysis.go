package bridge

import "context"

type AnalysisInspectRequest struct {
	DBDir        string   `json:"db_dir"`
	ContentIDs   []string `json:"content_ids"`
	PathPrefixes []string `json:"path_prefixes"`
	Playlist     string   `json:"playlist"`
	All          bool     `json:"all"`
}

type AnalysisContent struct {
	ID               string `json:"id"`
	Path             string `json:"path"`
	UUID             string `json:"uuid"`
	AnalysisDataPath string `json:"analysis_data_path"`
	ExistingDir      string `json:"existing_dir"`
	ANLZExists       bool   `json:"anlz_exists"`
	Analysed         int    `json:"analysed"`
}

type AnalysisInspectResponse struct {
	Contents    []AnalysisContent `json:"contents"`
	KeyNotation string            `json:"key_notation"`
}

type AnalysisResult struct {
	BPM               float64  `json:"bpm"`
	Key               string   `json:"key"`
	Camelot           string   `json:"camelot"`
	BeatCount         int      `json:"beat_count"`
	FirstBeatMS       *int     `json:"first_beat_ms"`
	TempoSegmentCount int      `json:"tempo_segment_count"`
	DurationSeconds   float64  `json:"duration_seconds"`
	Files             []string `json:"files"`
	Mode              string   `json:"mode"`
	AnalyserVersion   string   `json:"analyser_version"`
}

type AnalysisApplyRow struct {
	Content   AnalysisContent `json:"content"`
	OutputDir string          `json:"output_dir"`
	Result    AnalysisResult  `json:"result"`
}

type AnalysisApplyRequest struct {
	DBDir string             `json:"db_dir"`
	Rows  []AnalysisApplyRow `json:"rows"`
}

type AnalysisAppliedRow struct {
	ID               string `json:"id"`
	Error            string `json:"error,omitempty"`
	AnalysisDataPath string `json:"analysis_data_path,omitempty"`
}

type AnalysisApplyResponse struct {
	Rows []AnalysisAppliedRow `json:"rows"`
}

func (c Client) AnalysisInspect(ctx context.Context, req AnalysisInspectRequest) (AnalysisInspectResponse, error) {
	var resp AnalysisInspectResponse
	err := c.run(ctx, map[string]any{"op": "analysis_inspect", "request": req}, &resp)
	return resp, err
}

func (c Client) AnalysisApply(ctx context.Context, req AnalysisApplyRequest) (AnalysisApplyResponse, error) {
	var resp AnalysisApplyResponse
	err := c.run(ctx, map[string]any{"op": "analysis_apply", "request": req}, &resp)
	return resp, err
}
