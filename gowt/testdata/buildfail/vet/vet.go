package vet

import "fmt"

func BrokenFormat() {
	fmt.Printf("%d", "gowt-vet-marker")
}
