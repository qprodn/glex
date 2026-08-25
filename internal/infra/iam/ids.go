package iaminfra

import "sort"

func sortedIDs(ids []int64) []int64 {
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	unique := make([]int64, 0, len(set))
	for id := range set {
		unique = append(unique, id)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	return unique
}
