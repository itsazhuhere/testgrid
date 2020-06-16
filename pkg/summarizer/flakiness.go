package summarizer

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/GoogleCloudPlatform/testgrid/internal/result"
	"github.com/GoogleCloudPlatform/testgrid/pb/state"
	"github.com/sirupsen/logrus"
	"vbom.ml/util/sortorder"
)

var INFRA_REGEX = regexp.MustCompile(`^\w+$`)
var JAILED_REGEX = regexp.MustCompile(`(JAILED )?(\/\/[a-z0-9\/:-_]+) - \[([a-z0-9\/:-_.]+)\]\s`)

// Keep in mind that flakiness is measured as out of 100, i.e. 23 not .23
var DEFAULT_FLAKINESS = 50.0
var MIN_RUNS = 0
var HEALTHY_RANGE = map[int]string{
	20: "red",
	10: "purple",
	3:  "orange",
	0:  "green",
}

type Result struct {
	name             string
	passed           int
	failed           int
	flakyCount       int
	averageFlakiness float64
	failedInfraCount int
	infraFailures    map[string]int
}

// Temporary structs while I decide what will go into summary.proto
type Healthiness struct {
	startDate        int
	endDate          int
	tests            []TestInfo
	totalTests       int
	totalJailedTests int
	averageFlakiness float64
	flakyBuckets     []FlakyBucket
	infraIssues      map[string]int
	totalConfigs     int
}

type TestInfo struct {
	name               string
	env                string
	totalRuns          int
	totalRunsWithInfra int
	passedRuns         int
	failedRuns         int
	failedInfraRuns    int
	flakyRuns          int
	flakiness          float64
	infraInfo          string
}

type FlakyBucket struct {
	threshold float64
	tests     int
}

type IntString struct {
	s string
	i int
}

// Definitions to allow for
type IntStringSortable []IntString

func (list IntStringSortable) Len() int      { return len(list) }
func (list IntStringSortable) Swap(i, j int) { list[i], list[j] = list[j], list[i] }

func (list IntStringSortable) Less(i, j int) bool {
	if list[i].i < list[j].i {
		return true
	}
	if list[i].i > list[j].i {
		return false
	}
	return sortorder.NaturalLess(list[i].s, list[j].s)
}

func CalculateHealthiness(grid *state.Grid, startTime int, endTime int) Healthiness {
	results := parseGrid(grid, startTime, endTime)
	return analyzeFlakinessFromResults(results, startTime, endTime)
}

func parseGrid(grid *state.Grid, startTime int, endTime int) []Result {
	// Get the relevant data for flakiness from each Grid (which represents
	// a dashboard tab) as a list of Result structs

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make([]Result, 0)

	for _, test := range grid.Rows {
		// each row in Rows will have a field results
		// results is run length encoded
		// e.g. (encoded -> decoded equivalent):
		// [0, 3, 5, 4] -> [0, 0, 0, 5, 5, 5, 5]
		// decoded int values correspond to Row.Result enum
		var resultCounts Result
		resultCounts.infraFailures = make(map[string]int)
		i := -1
		for testResult := range result.Iter(ctx, test.Results) {
			i += 1
			if !isWithinTimeFrame(grid.Columns[i], startTime, endTime) {
				continue
			}
			switch rowResult := result.Coalesce(testResult, result.IgnoreRunning); rowResult {
			case state.Row_NO_RESULT:
				continue
			case state.Row_FAIL:
				categorizeFailure(&resultCounts, test.Messages[i])
			case state.Row_PASS:
				resultCounts.passed += 1
			case state.Row_FLAKY:
				getValueOfFlakyResult(&resultCounts)
			}
		}
		if resultCounts.failed > 0 || resultCounts.passed > 0 || resultCounts.flakyCount > 0 {
			resultCounts.name = test.Name
			results = append(results, resultCounts)
		}

	}
	return results
}

func analyzeFlakinessFromResults(results []Result, startTime int, endTime int) Healthiness {
	// TODO: minRuns
	return naiveFlakiness(results, MIN_RUNS, startTime, endTime)
}

func naiveFlakiness(results []Result, minRuns int, startDate int, endDate int) Healthiness {
	testByEnv := make(map[string]TestInfo)
	infraIssues := make(map[string]int)

	for _, test := range results {
		name := test.name
		env := ""
		match := JAILED_REGEX.FindStringSubmatch(name)
		if match != nil {
			name = match[1]
			env = match[2]
		} else {
			logrus.Infof("Test with name \"%s\" could not be split into name and env", name)
		}
		if len(test.infraFailures) > 0 {
			for errorType, errorCount := range test.infraFailures {
				infraIssues[test.name+"-"+errorType] += errorCount
			}
		}

		testInfo, success := calculateNaiveFlakiness(test, minRuns)
		if !success {
			continue
		}
		testInfo.name = name
		testInfo.env = env

		if currTestInfo, exists := testByEnv[name]; !exists || currTestInfo.flakiness < testInfo.flakiness {
			testByEnv[name] = testInfo
		}
	}
	// Populate Healthiness with above calculated information
	healthiness := createHealthiness(startDate, endDate, results, testByEnv, infraIssues)
	return healthiness
}

func createHealthiness(startDate int, endDate int, results []Result, testByEnv map[string]TestInfo, infraIssues map[string]int) Healthiness {
	healthiness := Healthiness{
		startDate:   startDate,
		endDate:     endDate,
		infraIssues: make(map[string]int),
	}

	// Go way of sorting the keys of a map in descending order
	var keys []int
	for k := range HEALTHY_RANGE {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(keys)))
	for threshold := range keys {
		newBucket := FlakyBucket{
			threshold: float64(threshold),
		}
		healthiness.flakyBuckets = append(healthiness.flakyBuckets, newBucket)
	}

	averageFlakiness := 0.0
	for _, testInfo := range testByEnv {
		healthiness.tests = append(healthiness.tests, testInfo)
		averageFlakiness += testInfo.flakiness
		for _, bucket := range healthiness.flakyBuckets {
			if testInfo.flakiness > bucket.threshold {
				bucket.tests += 1
			}
		}
	}
	healthiness.totalTests = len(healthiness.tests)
	healthiness.totalConfigs = len(results)
	if healthiness.totalTests > 0 {
		healthiness.averageFlakiness = averageFlakiness / float64(healthiness.totalTests)
	}
	for k, v := range infraIssues {
		healthiness.infraIssues[k] = v
	}
	return healthiness
}

func calculateNaiveFlakiness(test Result, minRuns int) (TestInfo, bool) {
	failedCount := test.failed
	totalCount := test.passed + test.failed
	totalCountWithInfra := totalCount + test.failedInfraCount
	if totalCount < minRuns {
		return TestInfo{}, false
	}
	flakiness := 100 * float64(failedCount) / float64(totalCount)
	infraInfo := calculateInfraInfo(test.infraFailures, test.failedInfraCount)
	testInfo := TestInfo{
		name:               "",
		env:                "",
		flakiness:          flakiness,
		totalRuns:          totalCount,
		totalRunsWithInfra: totalCountWithInfra,
		passedRuns:         test.passed,
		failedRuns:         test.failed,
		failedInfraRuns:    test.failedInfraCount,
		flakyRuns:          test.flakyCount,
		infraInfo:          infraInfo,
	}
	return testInfo, true

}

func calculateInfraInfo(issues map[string]int, failedCount int) string {
	result := make([]string, 0)
	if len(issues) > 0 && failedCount > 0 {
		items := IntStringSortable{}
		for key, value := range issues {
			items = append(items, IntString{
				s: key,
				i: value,
			})
		}
		sort.Sort(sort.Reverse(items))
		for k, v := range issues {
			result = append(result, k+fmt.Sprintf(" %.2f%% ", 100*float64(v)/float64(failedCount)))
		}
	}
	return strings.TrimSpace(strings.Join(result, ""))
}

func categorizeFailure(resultCounts *Result, message string) {
	if message == "" || !INFRA_REGEX.MatchString(message) {
		resultCounts.failed += 1
		return
	}
	resultCounts.failedInfraCount += 1
	resultCounts.infraFailures[message] += 1
}

func getValueOfFlakyResult(resultCounts *Result) {
	// Default behavior of adding a 50% flakiness
	flakiness := DEFAULT_FLAKINESS
	resultCounts.flakyCount += 1
	// Formula for adding one new value to mean is mean + (newValue - mean) / newCount
	resultCounts.averageFlakiness += (flakiness - resultCounts.averageFlakiness) / float64(resultCounts.flakyCount)
}

func isWithinTimeFrame(column *state.Column, startTime int, endTime int) bool {
	return column.Started >= float64(startTime) && column.Started <= float64(endTime)
}
