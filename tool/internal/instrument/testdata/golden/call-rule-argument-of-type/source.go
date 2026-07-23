// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func Handler(id int, name string) {
	fmt.Println(name)
}

func main() { Handler(42, "world") }
