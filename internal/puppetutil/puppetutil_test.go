package puppetutil

import (
	"fmt"
	"testing"
)

func TestFindNestCalls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    map[string]interface{}
		expected []interface{}
		find     string
	}{
		{
			map[string]interface{}{"^": map[string]interface{}{
				"^": []interface{}{"fozzie", "bear"},
			},
			},
			[]interface{}{"bear"},
			"fozzie",
		},
	}
	for _, test := range tests {
		actual, err := findNestedCalls(test.input, test.find, make([]interface{}, 0))
		if err != nil {
			t.Fatalf("findNestedCalls returned an unexpected error: %v", err)
			return
		}
		if actual[0] != test.expected[0] {
			t.Errorf("findNestedCalles diff expected:%s got:%s", test.expected[0], actual[0])
		}
	}
}

func TestFindNodeRegexes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    map[string]interface{}
		expected []string
	}{
		{
			map[string]interface{}{
				"oscar": "grouch",
				"#": []interface{}{
					[]interface{}{
						"yolanda", "rat",
					},
					[]interface{}{
						"gaffer",
					},
				},
			},
			[]string{"^gaffer"},
		},
		{
			map[string]interface{}{
				"oscar": "grouch",
				"#": []interface{}{
					[]interface{}{
						"yolanda", "rat",
					},
					[]interface{}{
						"gaffer", map[string]interface{}{
							"^": []interface{}{"regexp", "scooter$", "this", "is", "skipped"},
						},
					},
				},
			},
			[]string{"^gaffer", "scooter"},
		},
	}
	for _, test := range tests {
		actual, err := findNodeRegexes(test.input)
		fmt.Println(actual)
		if err != nil {
			t.Fatalf("findNodeRegexes returned an unexpected error: %v", err)
		}
		for x := range test.expected {
			if actual[x].String() != test.expected[x] {
				t.Fatalf("findNodeRegexes returned an unexpected error: %v", err)
			}
		}
	}
}
