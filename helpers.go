package containers

func SliceToSet(slice []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, k := range slice {
		set[k] = struct{}{}
	}

	return set
}
