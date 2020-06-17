package summarizer

import (
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/testgrid/pb/state"
)

func TestIsWithinTimeFrame(t *testing.T) {
	cases := []struct {
		name      string
		column    *state.Column
		startTime int
		endTime   int
		expected  bool
	}{
		{
			name: "column within time frame returns true",
			column: &state.Column{
				Started: 1.0,
			},
			startTime: 0,
			endTime:   2,
			expected:  true,
		},
		{
			name: "column outside of time frame returns false",
			column: &state.Column{
				Started: 4.0,
			},
			startTime: 0,
			endTime:   2,
			expected:  false,
		},
		{
			name: "function is inclusive with startTime",
			column: &state.Column{
				Started: 0.0,
			},
			startTime: 0,
			endTime:   2,
			expected:  true,
		},
		{
			name: "function is inclusive with endTime",
			column: &state.Column{
				Started: 2.0,
			},
			startTime: 0,
			endTime:   2,
			expected:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := isWithinTimeFrame(tc.column, tc.startTime, tc.endTime); actual != tc.expected {
				t.Errorf("actual %t != expect %t", actual, tc.expected)
			}
		})
	}
}

func TestParseGrid(t *testing.T) {
	cases := []struct {
		name      string
		grid      *state.Grid
		startTime int
		endTime   int
		expected  []Result
	}{
		{
			name: "grid with all analyzed result types produces correct result list",
			grid: &state.Grid{
				Columns: []*state.Column{
					{
						Started: 0,
					},
					{
						Started: 1,
					},
					{
						Started: 2,
					},
					{
						Started: 2,
					},
				},
				Rows: []*state.Row{
					{
						Name: "test_1",
						Results: []int32{
							state.Row_Result_value["PASS"], 1,
							state.Row_Result_value["FAIL"], 1,
							state.Row_Result_value["FLAKY"], 1,
							state.Row_Result_value["FAIL"], 1,
						},
						Messages: []string{
							"",
							"",
							"",
							"infra_fail_1",
						},
					},
				},
			},
			startTime: 0,
			endTime:   2,
			expected: []Result{
				{
					name:             "test_1",
					passed:           1,
					failed:           1,
					flakyCount:       1,
					averageFlakiness: 50.0,
					failedInfraCount: 1,
					infraFailures: map[string]int{
						"infra_fail_1": 1,
					},
				},
			},
		},
		{
			name: "grid with no analyzed results produces empty result list",
			grid: &state.Grid{
				Columns: []*state.Column{
					{
						Started: -1,
					},
					{
						Started: 1,
					},
					{
						Started: 2,
					},
					{
						Started: 2,
					},
				},
				Rows: []*state.Row{
					{
						Name: "test_1",
						Results: []int32{
							state.Row_Result_value["NO_RESULT"], 4,
						},
						Messages: []string{
							"this message should not show up in results_0",
							"this message should not show up in results_1",
							"this message should not show up in results_2",
							"this message should not show up in results_3",
						},
					},
				},
			},
			startTime: 0,
			endTime:   2,
			expected:  []Result{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := parseGrid(tc.grid, tc.startTime, tc.endTime); !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("\nactual %+v \n!= \nexpected %+v", actual, tc.expected)
			}
		})
	}
}

func getTypicalResultArray() []Result {
	return []Result{
		{
			name:             "//test1 - [env1]",
			passed:           1,
			failed:           1,
			flakyCount:       1,
			averageFlakiness: 50.0,
			failedInfraCount: 1,
			infraFailures: map[string]int{
				"infra_fail_1": 1,
			},
		},
	}
}

func TestNaiveFlakiness(t *testing.T) {
	cases := []struct {
		name      string
		results   []Result
		minRuns   int
		startDate int
		endDate   int
		tab       string
		expected  Healthiness
	}{
		{
			name:      "typical case returns expected healthiness",
			results:   getTypicalResultArray(),
			minRuns:   -1,
			startDate: 0,
			endDate:   2,
			tab:       "tab1",
			expected: Healthiness{
				startDate: 0,
				endDate:   2,
				tests: []TestInfo{
					{
						name:               "//test1",
						env:                "env1",
						totalRuns:          2,
						totalRunsWithInfra: 3,
						passedRuns:         1,
						failedRuns:         1,
						failedInfraRuns:    1,
						flakyRuns:          1,
						flakiness:          50,
						infraInfo:          "infra_fail_1 100.00%",
					},
				},
				totalTests:       1,
				totalJailedTests: 0,
				averageFlakiness: 50,
				flakyBuckets: []FlakyBucket{
					{
						threshold: float64(20),
						tests:     1,
					},
					{
						threshold: float64(10),
						tests:     0,
					},
					{
						threshold: float64(3),
						tests:     0,
					},
					{
						threshold: float64(0),
						tests:     0,
					},
				},
				infraIssues: map[string]int{
					"//test1 - [env1]-infra_fail_1": 1,
				},
				totalConfigs: 1,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := naiveFlakiness(tc.results, tc.minRuns, tc.startDate, tc.endDate, tc.tab); !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("\nactual %+v \n!= \nexpected %+v", actual, tc.expected)
			}
		})
	}
}

func TestGetNameAndEnvFromTest(t *testing.T) {
	cases := []struct {
		name             string
		testName         string
		tab              string
		expectedTestName string
		expectedEnv      string
	}{
		{
			name:             "string matchable by regex correctly returns parsed name and env",
			testName:         "//test1 - [env1]",
			tab:              "this should not appear in the output",
			expectedTestName: "//test1",
			expectedEnv:      "env1",
		},
		{
			name:             "JAILED tag in testName does not affect output",
			testName:         "JAILED //test1 - [env1]",
			tab:              "this should not appear in the output",
			expectedTestName: "//test1",
			expectedEnv:      "env1",
		},
		{
			name:             "string not matchable by regex correctly returns default values",
			testName:         "bleh",
			tab:              "defaultTab",
			expectedTestName: "bleh",
			expectedEnv:      "defaultTab",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actualTestName, actualEnv := getNameAndEnvFromTest(tc.testName, tc.tab)
			if actualTestName != tc.expectedTestName {
				t.Errorf("Test names don't match: \nactual: %s != expected: %s", actualTestName, tc.expectedTestName)
			}
			if actualEnv != tc.expectedEnv {
				t.Errorf("Env names don't match: \nactual: %s != expected: %s", actualEnv, tc.expectedEnv)
			}
		})
	}
}

func TestCreateHealthiness(t *testing.T) {
	cases := []struct {
		name        string
		startDate   int
		endDate     int
		results     []Result
		testByEnv   map[string]TestInfo
		infraIssues map[string]int
		expected    Healthiness
	}{
		{
			name:        "typical inputs return correct Healthiness output",
			startDate:   0,
			endDate:     2,
			results:     getTypicalResultArray(),
			testByEnv:   make(map[string]TestInfo),
			infraIssues: make(map[string]int),
			expected: Healthiness{
				startDate: 0,
				endDate:   2,
				tests:     []TestInfo{},
				flakyBuckets: []FlakyBucket{
					{
						threshold: float64(20),
						tests:     0,
					},
					{
						threshold: float64(10),
						tests:     0,
					},
					{
						threshold: float64(3),
						tests:     0,
					},
					{
						threshold: float64(0),
						tests:     0,
					},
				},
				infraIssues:  make(map[string]int),
				totalConfigs: 1,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := createHealthiness(tc.startDate, tc.endDate, tc.results, tc.testByEnv, tc.infraIssues); !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("\nactual: %+v \n!= \nexpected: %+v", actual, tc.expected)
			}
		})
	}
}

func TestCalculateNaiveFlakiness(t *testing.T) {
	cases := []struct {
		name             string
		test             Result
		minRuns          int
		expectedTestInfo TestInfo
		expectedSuccess  bool
	}{
		{
			name:             "correctly filters Result with less than minRuns",
			test:             Result{},
			minRuns:          1000, // arbitrarily large number so that it should get filtered
			expectedTestInfo: TestInfo{},
			expectedSuccess:  false,
		},
		{
			name: "typical Result returns correct TestInfo",
			test: Result{
				passed:           3,
				failed:           2,
				flakyCount:       8,
				averageFlakiness: 0.5,
				failedInfraCount: 4,
				infraFailures: map[string]int{
					"infra1": 3,
					"infra2": 1,
				},
			},
			minRuns: -1,
			expectedTestInfo: TestInfo{
				name:               "",
				env:                "",
				flakiness:          40.0,
				totalRuns:          5,
				totalRunsWithInfra: 9,
				passedRuns:         3,
				failedRuns:         2,
				failedInfraRuns:    4,
				flakyRuns:          8,
				infraInfo:          "infra1 75.00% infra2 25.00%",
			},
			expectedSuccess: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actualTest, actualSuccess := calculateNaiveFlakiness(tc.test, tc.minRuns); tc.expectedTestInfo != actualTest || tc.expectedSuccess != actualSuccess {
				t.Errorf("\ntestInfo:\nactual: %v vs. expected: %v\nsuccess:\nactual: %v vs. expected: %v", actualTest, tc.expectedTestInfo, actualSuccess, tc.expectedSuccess)
			}
		})
	}
}

func TestCalculateInfraInfo(t *testing.T) {
	cases := []struct {
		name        string
		issues      map[string]int
		failedCount int
		expected    string
	}{
		{
			name:        "empty issues map returns empty string",
			issues:      map[string]int{},
			failedCount: 10,
			expected:    "",
		},
		{
			name: "zero failedCount returns empty string",
			issues: map[string]int{
				"infra1": 2,
				"infra2": 7,
			},
			failedCount: 0,
			expected:    "",
		},
		{
			name: "typical issues map returns properly formatted string",
			issues: map[string]int{
				"infra1": 2,
				"infra2": 7,
			},
			failedCount: 10,
			expected:    "infra2 70.00% infra1 20.00%",
		},
		{
			name: "issues with same value count orders on key, ascending",
			issues: map[string]int{
				"infra1": 2,
				"infra2": 2,
			},
			failedCount: 10,
			expected:    "infra1 20.00% infra2 20.00%",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := calculateInfraInfo(tc.issues, tc.failedCount); actual != tc.expected {
				t.Errorf("actual: %s != expected: %s", actual, tc.expected)
			}
		})
	}
}
