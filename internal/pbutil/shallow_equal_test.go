package pbutil

import "testing"

type testStruct struct {
	inputA   any
	inputB   any
	expected bool
}

func testCore(t *testing.T, tests []*testStruct) {
	t.Helper()
	for i, test := range tests {
		result, err := ShallowEqual(test.inputA, test.inputB)
		if err != nil {
			t.Errorf("[Case %d ERROR] Unexpected error comparing %+v and %+v: %s", i, test.inputA, test.inputB, err)
			continue
		}
		if result == test.expected {
			t.Logf("[Case %d SUCCESS] Comparing %+v and %+v produced the expected result, %t", i, test.inputA, test.inputB, result)
		} else {
			t.Errorf("[Case %d FAIL] Comparing %+v and %+v produced %t, but should have been %t", i, test.inputA, test.inputB, result, test.expected)
		}
	}
}

func TestShallowEqualPrimitives(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{0, 0, true},
		{0, 1, false},
		{0.0, 0.0, true},
		{0.0, 1.0, false},
		{"hello", "hello", true},
		{"hello", "world", false},
	}
	testCore(t, tests)
}

type simpleStruct struct {
	int32
	float32
	string
}

func TestShallowEqualSimpleStructs(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			simpleStruct{0, 0.0, "hello"},
			simpleStruct{0, 0.0, "hello"},
			true,
		},
		{
			simpleStruct{0, 0.0, "hello"},
			simpleStruct{1, 0.0, "hello"},
			false,
		},
		{
			simpleStruct{0, 0.0, "hello"},
			simpleStruct{0, 1.0, "hello"},
			false,
		},
		{
			simpleStruct{0, 0.0, "hello"},
			simpleStruct{0, 0.0, "world"},
			false,
		},
	}
	testCore(t, tests)
}

type namedSimpleStruct struct {
	MyInt    int32
	MyFloat  float32
	MyString string
}

func TestShallowEqualNamedStructs(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			true,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			namedSimpleStruct{MyInt: 1, MyFloat: 0.0, MyString: "hello"},
			false,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 1.0, MyString: "hello"},
			false,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "world"},
			false,
		},
	}
	testCore(t, tests)
}

func TestShallowEqualCanCompareSameTypeMembersWithMissingNamesInOneStruct(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			simpleStruct{0, 0.0, "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			true,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			simpleStruct{0, 0.0, "hello"},
			true,
		},
		{
			simpleStruct{0, 0.0, "hello"},
			namedSimpleStruct{MyInt: 1, MyFloat: 0.0, MyString: "hello"},
			false,
		},
		{
			simpleStruct{0, 0.0, "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 1.0, MyString: "hello"},
			false,
		},
		{
			simpleStruct{0, 0.0, "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "world"},
			false,
		},
	}
	testCore(t, tests)
}

type simplerStruct struct {
	int32
}

type namedSimplerStruct struct {
	MyInt int32
}

func TestShallowEqualNeverTrueForUnequalMemberCounts(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			simpleStruct{0, 0.0, "hello"},
			simplerStruct{0},
			false,
		},
		{
			simplerStruct{0},
			simpleStruct{0, 0.0, "hello"},
			false,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			namedSimplerStruct{MyInt: 0},
			false,
		},
		{
			namedSimplerStruct{MyInt: 0},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			false,
		},
		{
			simpleStruct{0, 0.0, "hello"},
			namedSimplerStruct{MyInt: 0},
			false,
		},
		{
			simplerStruct{0},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			false,
		},
	}
	testCore(t, tests)
}

// remember: unname struct fields get called according to their type
type rearrangedSimpleStruct struct {
	string
	float32
	int32
}

type rearrangedNamedSimpleStruct struct {
	MyString string
	MyFloat  float32
	MyInt    int32
}

func TestShallowEqualRearrangedUnnamedMembers(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			simpleStruct{0, 0.0, "hello"},
			rearrangedSimpleStruct{"hello", 0.0, 0},
			true,
		},
		{
			rearrangedSimpleStruct{"hello", 0.0, 0},
			simpleStruct{0, 0.0, "hello"},
			true,
		},
		{
			simpleStruct{0, 0.0, "hello"},
			rearrangedSimpleStruct{"hello", 0.0, 1},
			false,
		},
		{
			rearrangedSimpleStruct{"world", 0.0, 0},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			false,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			rearrangedSimpleStruct{"world", 0.0, 0},
			false,
		},
		{
			simpleStruct{0, 0.0, "world"},
			rearrangedNamedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			false,
		},
		{
			rearrangedNamedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			simpleStruct{0, 0.0, "world"},
			false,
		},
	}
	testCore(t, tests)
}

func TestShallowEqualForRearrangedMembersWhenNamesStillMatch(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			rearrangedNamedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			true,
		},
		{
			rearrangedNamedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			true,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			rearrangedNamedSimpleStruct{MyInt: 1, MyFloat: 0.0, MyString: "hello"},
			false,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			rearrangedNamedSimpleStruct{MyInt: 0, MyFloat: 1.0, MyString: "hello"},
			false,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			rearrangedNamedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "world"},
			false,
		},
	}
	testCore(t, tests)
}

type renamedSimpleStruct struct {
	SomeInt    int32
	SomeFloat  float32
	SomeString string
}

type rearrangedRenamedSimpleStruct struct {
	SomeString string
	SomeFloat  float32
	SomeInt    int32
}

func TestShallowEqualTrueForRenamedMembersOnlyIfOrderMatches(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			renamedSimpleStruct{SomeInt: 0, SomeFloat: 0.0, SomeString: "hello"},
			true,
		},
		{
			renamedSimpleStruct{SomeInt: 0, SomeFloat: 0.0, SomeString: "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			true,
		},
		{
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			rearrangedRenamedSimpleStruct{SomeInt: 0, SomeFloat: 0.0, SomeString: "hello"},
			false,
		},
		{
			rearrangedRenamedSimpleStruct{SomeInt: 0, SomeFloat: 0.0, SomeString: "hello"},
			namedSimpleStruct{MyInt: 0, MyFloat: 0.0, MyString: "hello"},
			false,
		},
	}
	testCore(t, tests)
}

type nestedStruct struct {
	innerStruct simpleStruct
	extraValue  uint32
}

func TestShallowEqualNestedStructs(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			nestedStruct{
				innerStruct: simpleStruct{0, 0.0, "hello"},
				extraValue:  1,
			},
			nestedStruct{
				innerStruct: simpleStruct{0, 0.0, "hello"},
				extraValue:  1,
			},
			true,
		},
		{
			nestedStruct{
				innerStruct: simpleStruct{0, 0.0, "hello"},
				extraValue:  1,
			},
			nestedStruct{
				innerStruct: simpleStruct{0, 0.0, "world"},
				extraValue:  1,
			},
			false,
		},
		{
			nestedStruct{
				innerStruct: simpleStruct{0, 0.0, "hello"},
				extraValue:  1,
			},
			nestedStruct{
				innerStruct: simpleStruct{0, 0.0, "hello"},
				extraValue:  2,
			},
			false,
		},
	}
	testCore(t, tests)
}

func TestShallowEqualDereferencesPointers(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			&simpleStruct{0, 0.0, "hello"},
			&simpleStruct{0, 0.0, "hello"},
			true,
		},
		{
			&simpleStruct{0, 0.0, "hello"},
			&simpleStruct{1, 0.0, "hello"},
			false,
		},
		{
			&simpleStruct{0, 0.0, "hello"},
			&simpleStruct{0, 1.0, "hello"},
			false,
		},
		{
			&simpleStruct{0, 0.0, "hello"},
			&simpleStruct{0, 0.0, "world"},
			false,
		},
	}
	testCore(t, tests)
}

type simpleInterface interface {
	Foo(i any) bool
}

type SimpleInterfaceImplA struct {
	int32
}
type SimpleInterfaceImplB struct {
	int32
}

func (x SimpleInterfaceImplA) Foo(i any) any {
	return i
}

func (x SimpleInterfaceImplB) Foo(i any) any {
	return 1
}

func TestShallowEqualWorksWithDifferentTypesThatHaveTheSameShape(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		{
			SimpleInterfaceImplA{0},
			SimpleInterfaceImplB{0},
			true,
		},
		{
			SimpleInterfaceImplA{0},
			SimpleInterfaceImplB{1},
			false,
		},
		{
			SimpleInterfaceImplA{0},
			struct {
				int32
			}{0},
			true,
		},
		{
			SimpleInterfaceImplA{0},
			struct {
				int32
			}{1},
			false,
		},
	}
	testCore(t, tests)
}

func TestShallowEqualReturnsFalseForDivergingTypes(t *testing.T) {
	t.Parallel()
	tests := []*testStruct{
		// TBH I'm not sure if we should actually return false for equal values but divergent types,
		// but it's all the fault of reflect.Equal.
		// For heckler's internal purposes, that doesn't matter, so...
		{0, 0.0, false},
		{
			struct {
				int32
			}{0},
			struct {
				float32
			}{0.0},
			false,
		},
		{
			struct {
				int32
			}{0},
			struct {
				uint32
			}{0},
			false,
		},
		{
			struct {
				int32
			}{0},
			struct {
				string
			}{""},
			false,
		},
		{
			struct {
				MyInt int32
			}{0},
			struct {
				MyInt float32 // prove that field names don't matter if types diverge
			}{0},
			false,
		},
	}
	testCore(t, tests)
}

func TestShallowEqualRaisesErrorForNonComparables(t *testing.T) {
	t.Parallel()
	// https://cs.opensource.google/go/go/+/refs/tags/go1.20.3:src/reflect/value.go;l=3391
	a := make(map[string]int32)
	b := make(map[string]int32)
	tests := []*testStruct{
		{a, a, false},
		{a, b, false},
		{b, b, false},
		{testCore, testCore, false},
	}
	for _, test := range tests {
		result, err := ShallowEqual(test.inputA, test.inputB)
		if err == nil {
			t.Errorf("Expected error saying that these inputs were not supported, but didn't get one. Compared %#v and %#v, result was %t", a, b, result)
		}
	}
}
