package utils

func MergeMap(mObj ...map[string]any) map[string]any {
	newObj := map[string]any{}
	for _, m := range mObj {
		for k, v := range m {
			newObj[k] = v
		}
	}
	return newObj
}