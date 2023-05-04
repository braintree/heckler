// Package pbutil provides utility functions for dealing with quirks of the
// auto-generated code from `protoc`.
package pbutil

import (
	"errors"
	"reflect"
)

// ShallowEqual uses reflection to compare two things in a shallow
// (not deep like `reflect.DeepEqual`) way.
//
// If they're pointers, they're dereferenced first before proceeding to
// the rest of the logic below.
//
// If they're structs, it does `Value.Equal()` for all visible fields whose
// types are `.Comparable()`, and ignores any other fields. When comparing
// fields, there must be the same number of visible comparable fields, and
// they must all be equal to each other. This function allows for comparing
// by field declaration order or by field name; it doesn't care if one struct
// has unnamed fields and the other has named ones if their fields are equal
// to each other in the order they're declared, and it doesn't care if both
// structs use different names for the same types of their fields if those
// types happen to be in the same order. Since unnamed fields actually get
// named according to their type, order of field declaration only matters
// when names aren't identical between the two structs. This allows for some
// amount of reflective polymorphism where two structs that contain the same
// data, even if they label that data differently, generally can be considered
// equal, but if the declaration order and names are both all jumbled up, we
// don't bother trying to build a 1:1 mapping since that would be very
// inefficient and cover very few legitimate use cases. In more concrete terms,
// using the below example types:
//
//   - Instances of A and instances of B can be equal because they both declare
//     an int32 and a string in that order, even though only B uses names.
//   - Instances of A can be equal with instances of C because the names that
//     the fields get implicitly are the same between A and C, even if the
//     delcaration order is different.
//   - Instances of B cannot be equal with instances of C because both the
//     names and the declaration order are different.
//   - Instances of A cannot be equal with instances of D for the same reason
//     that instances of B cannot be equal with instances of C.
//   - However, instances of B *can* be equal with instances of D, since they
//     both use the same names for the same types, despite declaring them in
//     inverted order.
//   - Instances of C can be equal with instances of D or E for the same
//     reason that instances of A and instances of B can be equal.
//   - Instances of D can also be equal with instances of E because they
//     declare the same type of fields in the same order, despite using
//     different names for the fields.
//   - However, instances of D cannot be equal with instances of B because
//     they use different names and in a different order.
//   - Instances of F cannot be equal with instances of any other type because
//     F only has one field whereas every other type has two.
//   - Instances of G cannot be equal with instances of any other type because
//     G uses a field type that none of the other types do (i.e., `float32` vs
//     the `int32` in everything else.) Even though G named its first field
//     `MyInt`, it is not be considered equal to an `int32` (since this
//     function uses `Value.Equal()` to compare the field values.)
//
// ```
//
//	type A struct {
//		int32
//		string
//	}
//
//	type B struct {
//		MyInt int32
//		MyString string
//	}
//
//	type C struct {
//		string
//		int32
//	}
//
//	type D struct {
//		MyString string
//		MyInt int32
//	}
//
//	type E struct {
//		SomeString string
//		SomeInt int32
//	}
//
//	type F struct {
//		MyInt int32
//	}
//
//	type G struct {
//		MyInt float32	// intentionally lying about the type with this name
//		MyString string
//	}
//
// ```
//
// Otherwise, if they're some other type that is `.Comparable()`, it just
// does `Value.Equal()` and hopes for the best.
//
// If none of the above apply, it returns false.
//
// Superceding any of the above, if an error occurs, false will be returned
// along with the error.
//
// Note that this method is different from `reflect.Equal` in that it does not
// care if the types of the arguments are the same, only their `Kind`s.
func ShallowEqual(a, b any) (equal bool, err error) {
	aVal, bVal := reflect.Indirect(reflect.ValueOf(a)), reflect.Indirect(reflect.ValueOf(b))
	if aVal.Kind() == reflect.Struct && bVal.Kind() == reflect.Struct {
		aFields, bFields := reflect.VisibleFields(aVal.Type()), reflect.VisibleFields(bVal.Type())
		aComparableFields, bComparableFields := Filter(aFields, fieldTypeIsComparable), Filter(bFields, fieldTypeIsComparable)
		if len(aComparableFields) != len(bComparableFields) {
			return
		}
		// assume equal until proven otherwise
		equal = true
		// check by index first, since it's easier
		for i, aField := range aComparableFields {
			aFieldVal, aErr := aVal.FieldByIndexErr(aField.Index)
			bFieldVal, bErr := bVal.FieldByIndexErr(bComparableFields[i].Index)
			err = errors.Join(err, aErr, bErr)
			equal = equal && (err == nil) && aFieldVal.Equal(bFieldVal)
		}
		// check by name only if index check didn't succeed, since name check is more complex
		// (all thanks to FieldByName returning a zero value if the field's not found)
		if equal {
			return
		}
		equal = true
		aFieldsMap := fieldNameToFieldValueMap(aVal, &aComparableFields)
		bFieldsMap := fieldNameToFieldValueMap(bVal, &bComparableFields)
		for aFieldName, aFieldVal := range aFieldsMap {
			bFieldVal, bAlsoHasField := bFieldsMap[aFieldName]
			if equal = bAlsoHasField && aFieldVal.Equal(bFieldVal); !equal {
				break
			}
		}
	} else if aVal.Comparable() && bVal.Comparable() {
		equal = aVal.Equal(bVal)
	} else {
		err = errors.New("ShallowEqual does not support things that are neither structs nor Comparable via `reflect`")
	}
	return
}

// Filter is a basic functional programming concept that a language like
// Golang should have already built into its standard library by now since it
// has first-order functions and some semblance of polymorphism, but it doesn't
// for some reason.
func Filter[T any](values []T, filter func(T) bool) []T {
	filtered := make([]T, 0, len(values))
	for _, value := range values {
		if filter(value) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func fieldTypeIsComparable(field reflect.StructField) bool {
	return field.Type.Comparable()
}

func fieldNameToFieldValueMap(val reflect.Value, fields *[]reflect.StructField) (fieldsMap map[string]reflect.Value) {
	fieldsMap = make(map[string]reflect.Value, len(*fields))
	for _, field := range *fields {
		fieldsMap[field.Name] = val.FieldByName(field.Name)
	}
	return
}
