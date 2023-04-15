package util

import (
	"errors"
	"reflect"
)

// ShallowEqual compares two things in a shallow (not deep like reflect.DeepEqual) way.
// if they're pointers, they're dereferenced first before proceeding.
// if they're structs, it does Value.Equal() for all visible fields whose types are .Comparable(), and ignores any other fields.
// otherwise, if they're some other type that is .Comparable(), it just does Value.Equal() and hopes for the best.
// if none of the above apply, it returns false.
// superceding any of the above, if an error occurs, false will be returned along with the error.
// note that this method is different from reflect.Equal in that it does not care if the types are the same, only the kinds.
func ShallowEqual(a, b any) (bool, error) {
	aVal, bVal := reflect.Indirect(reflect.ValueOf(a)), reflect.Indirect(reflect.ValueOf(b))
	if aVal.Kind() == reflect.Struct && bVal.Kind() == reflect.Struct {
		aFields, bFields := reflect.VisibleFields(aVal.Type()), reflect.VisibleFields(bVal.Type())
		aComparableFields, bComparableFields := Filter(aFields, fieldTypeIsComparable), Filter(bFields, fieldTypeIsComparable)
		if len(aComparableFields) != len(bComparableFields) {
			return false, nil
		}
		for i, aField := range aComparableFields {
			aFieldVal, aErr := aVal.FieldByIndexErr(aField.Index)
			bFieldVal, bErr := bVal.FieldByIndexErr(bComparableFields[i].Index)
			if err := errors.Join(aErr, bErr); err != nil {
				return false, err
			}
			if !aFieldVal.Equal(bFieldVal) {
				return false, nil
			}
		}
		return true, nil
	} else if aVal.Comparable() && bVal.Comparable() {
		return aVal.Equal(bVal), nil
	} else {
		return false, errors.New("ShallowEqual does not support things that are neither structs nor Comparable via `reflect`")
	}
}

// lol golang still makes you roll your own basic functional programming tools
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
