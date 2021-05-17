package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func lessStr(x, y string) bool {
	return x < y
}

func TestGroupedResourceNodeFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    []*groupedResource
		expected []*groupedResource
	}{
		{
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/cast]",
					File:  "",
					Hosts: []string{"fozzie.example.com", "statler.example.com", "waldorf.example.com"},
				},
			},
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/cast]",
					File:  "",
					Hosts: []string{"fozzie.example.com", "statler.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/statler.pp",
						"nodes/waldorf.pp",
					},
				},
			},
		},
	}
	for _, test := range tests {
		actual, err := groupedResourcesNodeFiles(test.input, "../../muppetshow")
		if err != nil {
			t.Fatalf("groupedResourcesNodeFiles returned an unexpected error: %v", err)
			return
		}
		if diff := cmp.Diff(test.expected, actual, cmpopts.SortSlices(lessStr)); diff != "" {
			t.Errorf("groupedResourcesNodeFiles() mismatch (-expected +actual):\n%s", diff)
		}
	}
}

func TestGroupedResourceOwners(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    []*groupedResource
		expected []*groupedResource
	}{
		{
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/cast]",
					File:  "",
					Hosts: []string{"fozzie.example.com", "statler.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/statler.pp",
						"nodes/waldorf.pp",
					},
				},
			},
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/cast]",
					File:  "",
					Hosts: []string{"fozzie.example.com", "statler.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/statler.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						File: nil,
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@braintree/muppets"},
							"nodes/statler.pp": {"@braintree/muppets"},
							"nodes/waldorf.pp": {"@braintree/muppets"},
						},
					},
				},
			},
		},
		{
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Hosts: []string{"fozzie.example.com", "statler.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/statler.pp",
						"nodes/waldorf.pp",
					},
				},
			},
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Hosts: []string{"fozzie.example.com", "statler.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/statler.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						File: []string{"@misspiggy"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@braintree/muppets"},
							"nodes/statler.pp": {"@braintree/muppets"},
							"nodes/waldorf.pp": {"@braintree/muppets"},
						},
					},
				},
			},
		},
		{
			[]*groupedResource{
				&groupedResource{
					Title: "Group[gonzo]",
					Module: Module{
						Name: "muppetshow",
						Path: "modules/muppetshow",
					},
				},
			},
			[]*groupedResource{
				&groupedResource{
					Title: "Group[gonzo]",
					Module: Module{
						Name: "muppetshow",
						Path: "modules/muppetshow",
					},
					Owners: groupedResourceOwners{
						File:      nil,
						Module:    []string{"@kermit"},
						NodeFiles: map[string][]string{},
					},
				},
			},
		},
	}
	for _, test := range tests {
		actual, err := groupedResourcesOwners(test.input, "../../muppetshow")
		if err != nil {
			t.Fatalf("groupedResourcesOwners returned an unexpected error: %v", err)
			return
		}
		if diff := cmp.Diff(test.expected, actual, cmpopts.SortSlices(lessStr)); diff != "" {
			t.Errorf("groupedResourcesOwners() mismatch (-expected +actual):\n%s", diff)
		}
	}
}

func TestResourcesApproved(t *testing.T) {
	t.Parallel()
	type resourcesApprovedInput struct {
		gr        []*groupedResource
		approvers []string
		groups    map[string][]string
	}
	type resourcesApprovedExpected struct {
		approved bool
		gr       []*groupedResource
	}
	type resourcesApprovedTest struct {
		input    resourcesApprovedInput
		expected resourcesApprovedExpected
	}
	tests := make([]resourcesApprovedTest, 0)
	testFileOwner := resourcesApprovedTest{
		resourcesApprovedInput{
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Hosts: []string{"fozzie.example.com", "statler.example.com", "waldorf.example.com"},
					Owners: groupedResourceOwners{
						File: []string{"@misspiggy"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@braintree/muppets"},
							"nodes/statler.pp": nil,
							"nodes/waldorf.pp": {"@braintree/muppets"},
						},
					},
				},
			},
			[]string{"@misspiggy"},
			map[string][]string{},
		},
		resourcesApprovedExpected{
			true,
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Hosts: []string{"fozzie.example.com", "statler.example.com", "waldorf.example.com"},
					Owners: groupedResourceOwners{
						File: []string{"@misspiggy"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@braintree/muppets"},
							"nodes/statler.pp": nil,
							"nodes/waldorf.pp": {"@braintree/muppets"},
						},
					},
					Approved: "Source File Approved",
					Approvals: groupedResourceApprovals{
						File: []string{"@misspiggy"},
					},
				},
			},
		},
	}
	tests = append(tests, testFileOwner)
	testNodeOwners := resourcesApprovedTest{
		resourcesApprovedInput{
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						File: []string{"@misspiggy"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@braintree/muppets"},
							"nodes/waldorf.pp": {"@braintree/muppets"},
						},
					},
				},
			},
			[]string{"@kermit"},
			map[string][]string{
				"@braintree/muppets": []string{"@kermit", "@misspiggy"},
			},
		},
		resourcesApprovedExpected{
			true,
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						File: []string{"@misspiggy"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@braintree/muppets"},
							"nodes/waldorf.pp": {"@braintree/muppets"},
						},
					},
					Approved: "Nodes Approved",
					Approvals: groupedResourceApprovals{
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@kermit"},
							"nodes/waldorf.pp": {"@kermit"},
						},
					},
				},
			},
		},
	}
	tests = append(tests, testNodeOwners)
	testModuleOwners := resourcesApprovedTest{
		resourcesApprovedInput{
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Module: Module{
						Name: "muppetshow",
						Path: "modules/muppetshow",
					},
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						File:   []string{"@misspiggy"},
						Module: []string{"@kermit"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@misspiggy"},
							"nodes/waldorf.pp": {"@misspiggy"},
						},
					},
				},
			},
			[]string{"@kermit"},
			map[string][]string{
				"@braintree/muppets": []string{"@kermit", "@misspiggy"},
			},
		},
		resourcesApprovedExpected{
			true,
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					Module: Module{
						Name: "muppetshow",
						Path: "modules/muppetshow",
					},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						File:   []string{"@misspiggy"},
						Module: []string{"@kermit"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@misspiggy"},
							"nodes/waldorf.pp": {"@misspiggy"},
						},
					},
					Approved: "Module Approved",
					Approvals: groupedResourceApprovals{
						Module: []string{"@kermit"},
					},
				},
			},
		},
	}
	tests = append(tests, testModuleOwners)
	for _, test := range tests {
		approved := resourcesApproved(test.input.gr, test.input.groups, test.input.approvers)
		if test.expected.approved != approved {
			t.Errorf("resourcesApproved() mismatch expected '%v' actual '%v'", test.expected.approved, approved)
		}
		if diff := cmp.Diff(test.expected.gr, test.input.gr, cmpopts.SortSlices(lessStr)); diff != "" {
			t.Errorf("resourcesApproved() mismatch (-expected +actual):\n%s", diff)
		}
	}
}

func TestIntersectionOwnersApprovers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		inputOwners    []string
		inputApprovers []string
		inputGroups    map[string][]string
		expected       []string
	}{
		{
			[]string{"@foo", "@bar"},
			[]string{"@foo", "@biz", "@baz"},
			map[string][]string{},
			[]string{"@foo"},
		},
		{
			[]string{"@foo", "@org/b"},
			[]string{"@foo", "@butter", "@bubbles"},
			map[string][]string{
				"@org/b": []string{"@butter", "@bubbles"},
			},
			[]string{"@foo", "@butter", "@bubbles"},
		},
	}
	for _, test := range tests {
		actual := intersectionOwnersApprovers(test.inputOwners, test.inputApprovers, test.inputGroups)
		if diff := cmp.Diff(test.expected, actual, cmpopts.SortSlices(lessStr)); diff != "" {
			t.Errorf("intersectionOwnersApprovers() mismatch (-expected +actual):\n%s", diff)
		}
	}
}

func TestNextSemVerTags(t *testing.T) {
	t.Parallel()
	type input struct {
		priorTag string
		prefix   string
		tags     []string
	}
	tests := []struct {
		input    input
		expected []string
	}{
		{
			input{
				priorTag: "v1",
				prefix:   "",
				tags:     []string{"v1", "v1.1", "v2"},
			},
			[]string{"v1.1", "v2"},
		},
	}
	for _, test := range tests {
		actual, err := nextSemVerTags(test.input.priorTag, test.input.prefix, test.input.tags)
		if err != nil {
			t.Fatalf("nextSemVerTag returned an unexpected error: %v", err)
			return
		}
		if diff := cmp.Diff(test.expected, actual); diff != "" {
			t.Errorf("nextSemVerTag() mismatch (-expected +actual):\n%s", diff)
		}
	}
}

func TestResourceIgnored(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected bool
	}{
		{
			input:    "Exec[sl]",
			expected: true,
		},
		{
			input:    "Exec[ls]",
			expected: false,
		},
		{
			input:    "File[sl]",
			expected: false,
		},
		{
			input:    "File[/etc/hosts]",
			expected: true,
		},
		{
			input:    "Pg_user[butter_ro]",
			expected: true,
		},
		{
			input:    "Pg_user[bubbles]",
			expected: false,
		},
	}
	roDbUser, _ := SerializedRegexpCompile("^.*_ro$")
	ignoredResources := []IgnoredResources{
		IgnoredResources{
			Purpose:   "Humor",
			Rationale: "Ride that train!",
			Resources: []Resource{
				Resource{
					Type:  "Exec",
					Title: "sl",
				},
			},
		},
		IgnoredResources{
			Purpose:   "IP mapping",
			Rationale: "DNS is the dream",
			Resources: []Resource{
				Resource{
					Type:  "File",
					Title: "/etc/hosts",
				},
			},
		},
		IgnoredResources{
			Purpose:   "Readonly DB user",
			Rationale: "Love that SQL!",
			Resources: []Resource{
				Resource{
					Type:       "Pg_user",
					TitleRegex: roDbUser,
				},
			},
		},
	}
	for _, test := range tests {
		actual, err := resourceIgnored(test.input, ignoredResources)
		if err != nil {
			t.Fatalf("resourceIgnored returned an unexpected error: %v", err)
			return
		}
		if diff := cmp.Diff(test.expected, actual); diff != "" {
			t.Errorf("resourceIgnored() mismatch (-expected +actual):\n%s", diff)
		}
	}
}

func TestOwnersNeeded(t *testing.T) {
	t.Parallel()
	type ownersNeededInput struct {
		gr     []*groupedResource
		admins []string
	}
	type ownersNeededTest struct {
		input    ownersNeededInput
		expected []string
	}
	tests := make([]ownersNeededTest, 0)
	testOwners := ownersNeededTest{
		ownersNeededInput{
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Module: Module{
						Name: "muppetshow",
						Path: "modules/muppetshow",
					},
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						File: []string{"@fozzie"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@braintree/muppets"},
							"nodes/waldorf.pp": {"@braintree/muppets"},
						},
					},
					Approved: resourceSourceFileApproved,
				},
				&groupedResource{
					Title: "Exec[/bin/sl]",
					File:  "modules/fozzie/manifests/init.pp",
					Module: Module{
						Name: "fozzie",
						Path: "modules/fozzie",
					},
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						Module: []string{"@braintree/muppets"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@misspiggy"},
							"nodes/waldorf.pp": {"@misspiggy"},
						},
					},
					Approved: resourceNotApproved,
				},
			},
			[]string{"@ralph"},
		},
		[]string{"@braintree/muppets"},
	}
	tests = append(tests, testOwners)
	testAdminOwners := ownersNeededTest{
		ownersNeededInput{
			[]*groupedResource{
				&groupedResource{
					Title: "File[/data/puppet_apply/laughtrack]",
					File:  "modules/muppetshow/manifests/episode.pp",
					Module: Module{
						Name: "muppetshow",
						Path: "modules/muppetshow",
					},
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners:   groupedResourceOwners{},
					Approved: resourceNotApproved,
				},
			},
			[]string{"@ralph"},
		},
		[]string{"@ralph"},
	}
	tests = append(tests, testAdminOwners)
	testNodeOwners := ownersNeededTest{
		ownersNeededInput{
			[]*groupedResource{
				&groupedResource{
					Title: "Exec[/bin/sl]",
					File:  "modules/fozzie/manifests/init.pp",
					Module: Module{
						Name: "fozzie",
						Path: "modules/fozzie",
					},
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@misspiggy"},
							"nodes/waldorf.pp": {"@misspiggy"},
						},
					},
					Approved: resourceNotApproved,
				},
			},
			[]string{"@ralph"},
		},
		[]string{"@misspiggy"},
	}
	tests = append(tests, testNodeOwners)
	testFileOwners := ownersNeededTest{
		ownersNeededInput{
			[]*groupedResource{
				&groupedResource{
					Title: "Exec[/bin/sl]",
					File:  "modules/fozzie/manifests/init.pp",
					Module: Module{
						Name: "fozzie",
						Path: "modules/fozzie",
					},
					Hosts: []string{"fozzie.example.com", "waldorf.example.com"},
					NodeFiles: []string{
						"nodes/fozzie.pp",
						"nodes/waldorf.pp",
					},
					Owners: groupedResourceOwners{
						File:   []string{"@fozzie"},
						Module: []string{"@braintree/muppets"},
						NodeFiles: map[string][]string{
							"nodes/fozzie.pp":  {"@misspiggy"},
							"nodes/waldorf.pp": {"@misspiggy"},
						},
					},
					Approved: resourceNotApproved,
				},
			},
			[]string{"@ralph"},
		},
		[]string{"@fozzie"},
	}
	tests = append(tests, testFileOwners)
	for _, test := range tests {
		actual := ownersNeeded(test.input.gr, test.input.admins)
		if diff := cmp.Diff(test.expected, actual); diff != "" {
			t.Errorf("nextSemVerTag() mismatch (-expected +actual):\n%s", diff)
		}
	}
}
