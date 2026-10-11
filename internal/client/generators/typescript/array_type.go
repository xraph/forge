package typescript

import "strings"

func tsArrayType(itemType string) string {
	if strings.ContainsAny(itemType, "|&") {
		return "(" + itemType + ")[]"
	}

	return itemType + "[]"
}
