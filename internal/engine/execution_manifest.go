package engine

import "fmt"

type ExecutionEntry struct {
	Index         int
	RemoteID      string
	Title         string
	ExecutionSlot int
}

type ExecutionManifest struct {
	SourceID        string
	DownloadOrder   DownloadOrder
	SelectedIndices []int
	Execution       []ExecutionEntry
}

func DefaultSelectedPlanIndices(rows []PlanRow) []int {
	selected := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.Toggleable && row.SelectedByDefault {
			selected = append(selected, row.Index)
		}
	}
	return selected
}

func BuildExecutionManifest(sourceID string, rows []PlanRow, selectedIndices []int, order DownloadOrder) (ExecutionManifest, error) {
	indexSet := map[int]struct{}{}
	for _, idx := range selectedIndices {
		if idx <= 0 {
			return ExecutionManifest{}, fmt.Errorf("invalid selected index %d", idx)
		}
		indexSet[idx] = struct{}{}
	}

	selected := make([]int, 0, len(indexSet))
	entriesByIndex := map[int]ExecutionEntry{}
	validSelectionIndices := map[int]struct{}{}
	for _, row := range rows {
		if !row.Toggleable {
			continue
		}
		validSelectionIndices[row.Index] = struct{}{}
		if _, ok := indexSet[row.Index]; !ok {
			continue
		}
		selected = append(selected, row.Index)
		entriesByIndex[row.Index] = ExecutionEntry{
			Index:    row.Index,
			RemoteID: row.RemoteID,
			Title:    row.Title,
		}
	}

	for idx := range indexSet {
		if _, ok := validSelectionIndices[idx]; !ok {
			return ExecutionManifest{}, fmt.Errorf("selected index %d is not toggleable for this source", idx)
		}
	}

	executionIndices := orderForExecution(selected, NormalizeDownloadOrder(order))
	execution := make([]ExecutionEntry, 0, len(executionIndices))
	for i, idx := range executionIndices {
		entry := entriesByIndex[idx]
		entry.ExecutionSlot = i + 1
		execution = append(execution, entry)
	}

	return ExecutionManifest{
		SourceID:        sourceID,
		DownloadOrder:   NormalizeDownloadOrder(order),
		SelectedIndices: selected,
		Execution:       execution,
	}, nil
}
