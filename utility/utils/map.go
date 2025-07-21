package utils

import "maps"

func MergeMap(mObj ...map[string]any) map[string]any {
	newObj := map[string]any{}
	for _, m := range mObj {
		maps.Copy(newObj, m)
	}
	return newObj
}
