package main

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		v1, v2   string
		expected bool
	}{
		{"10.1.1", "2.0.0", true},
		{"1.10.0", "1.9.0", true},
		{"1.2.3", "1.2.3", false},
		{"0.9.5", "1.0.0", false},
	}

	for _, tc := range cases {
		result := compareVersions(tc.v1, tc.v2)
		if result == tc.expected {
			t.Logf(
				"Test Passed: compareVersions(%s, %s) = %v (Expected: %v)\n",
				tc.v1,
				tc.v2,
				result,
				tc.expected,
			)

		} else {
			t.Errorf("Test Failed: compareVersions(%s, %s) = %v; Expected %v", tc.v1, tc.v2, result, tc.expected)
		}
	}
}
