package main

import (
	"fmt"

	"github.com/sounddock/sounddock/migrations"
)

func main() {
	fmt.Println(migrations.Head())
}
