package summarizer

import (
	"testing"

	"github.com/GoogleCloudPlatform/testgrid/pb/state"
)

func TestSomething(t *testing.T) {

}

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

			if actual := IsWithinTimeFrame(tc.column, tc.startTime, tc.endTime); actual != tc.expected {
				t.Errorf("actual %d != expect %d", actual, tc.expected)
			}
		})
	}
}
