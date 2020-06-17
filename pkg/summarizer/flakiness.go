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
var JAILED_REGEX = regexp.MustCompile(`(JAILED )?(\/?\/?[A-Za-z0-9\/_\-.:}{]+) - \[([A-Za-z0-9\/:\-_.\s]+)\]\s*`)

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

// For sorting, primarily intended for map[string]int as implemented below
type IntString struct {
	i int
	s string
}

func CalculateHealthiness(grid *state.Grid, startTime int, endTime int, tab string) Healthiness {
	results := parseGrid(grid, startTime, endTime)
	return analyzeFlakinessFromResults(results, startTime, endTime, tab)
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

func analyzeFlakinessFromResults(results []Result, startTime int, endTime int, tab string) Healthiness {
	return naiveFlakiness(results, MIN_RUNS, startTime, endTime, tab)
}

func naiveFlakiness(results []Result, minRuns int, startDate int, endDate int, tab string) Healthiness {
	testByEnv := make(map[string]TestInfo)
	infraIssues := make(map[string]int)

	for _, test := range results {
		name, env := getNameAndEnvFromTest(test.name, tab)
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

func getNameAndEnvFromTest(testName string, tabName string) (string, string) {
	name := testName
	env := tabName
	match := JAILED_REGEX.FindStringSubmatch(name)
	if match != nil {
		name = match[2]
		env = match[3]
	} else {
		logrus.Infof("Test \"%s\" could not be split into name and env, using tab name \"%s\" as its env", testName, tabName)
	}
	return name, env
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

func createHealthiness(startDate int, endDate int, results []Result, testByEnv map[string]TestInfo, infraIssues map[string]int) Healthiness {
	healthiness := Healthiness{
		startDate:   startDate,
		endDate:     endDate,
		tests:       []TestInfo{},
		infraIssues: make(map[string]int),
	}

	// The Go way of sorting the keys of a map in descending order
	var keys []int
	for k := range HEALTHY_RANGE {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(keys)))
	for _, threshold := range keys {
		newBucket := FlakyBucket{
			threshold: float64(threshold),
		}
		healthiness.flakyBuckets = append(healthiness.flakyBuckets, newBucket)
	}

	averageFlakiness := 0.0
	for _, testInfo := range testByEnv {
		healthiness.tests = append(healthiness.tests, testInfo)
		averageFlakiness += testInfo.flakiness
		for i, bucket := range healthiness.flakyBuckets {
			if testInfo.flakiness > bucket.threshold {
				healthiness.flakyBuckets[i].tests += 1.0
				break
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

func calculateInfraInfo(issues map[string]int, failedCount int) string {
	result := make([]string, 0)
	if len(issues) > 0 && failedCount > 0 {
		// Sorts the map items by value (int) and then key (string) if values are equal
		// The sort is in descending order: [5,4,3]
		items := make([]IntString, 0)
		for key, value := range issues {
			items = append(items, IntString{
				s: key,
				i: value,
			})
		}
		sort.Slice(items, func(i, j int) bool {
			// These two comparisons enforce descending order for the integers
			if items[i].i > items[j].i {
				return true
			}
			if items[i].i < items[j].i {
				return false
			}
			// String comparison is still ascending: [a,b,c]
			return sortorder.NaturalLess(items[i].s, items[j].s)
		})
		for _, item := range items {
			result = append(result, item.s+fmt.Sprintf(" %.2f%% ", 100*float64(item.i)/float64(failedCount)))
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
