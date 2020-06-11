package summarizer

import (
	"context"

	"github.com/GoogleCloudPlatform/testgrid/internal/result"
	"github.com/GoogleCloudPlatform/testgrid/pb/state"
)

type Result struct {
	name          string
	passed        int
	failed        int
	failedInfra   int
	infraFailures int
}

func AnalyzeFlakiness(grid *state.Grid, timeInterval int) {

}

func ParseGrid(grid *state.Grid, startTime int, endTime int) {
	// Get the relevant data for flakiness from each Grid (which represents
	// a dashboard tab)

	// grid.Columns will have column info, specifically timestamps
	// we can filter timestamps first instead of doing it row by row
	// can we assume that timestamps are in order?

	// We know that result.Iter will act as an iterable, so we can avoid accessing the
	// first half of the iterable twice by determining which indices will correspond to which
	// time spans
	// e.g [startCurrent to endCurrent], [endCurrent + 1 == startPrev to endPrev

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make([]string, 0)

	for _, test := range grid.Rows {
		// each row in Rows will have a field results
		// results is run length encoded
		// e.g. (encoded -> decoded equivalent):
		// [0, 3, 5, 4] -> [0, 0, 0, 5, 5, 5, 5]
		// decoded int values correspond to Row.Result enum

		// There's no guarantee that Grid.Columns is in order, although it most likely is in order
		for i, testRun := range result.Map(ctx, test) {
			if !IsWithinTimeFrame(grid.Columns[i], startTime, endTime) {
				continue
			}
			switch rowResult := result.Coalesce(test); rowResult {
			case state.Row_FAIL:

			}
		}
		// switch on test.status against status.TestStatus and increment Result.passed/failed based on value

	}

}

func IsWithinTimeFrame(column *state.Column, startTime int, endTime int) bool {
	// TODO: check what column.started represents (seconds or milliseconds)
	return column.Started >= float64(startTime) && column.Started <= float64(endTime)
}

func ParseRow() {

}
