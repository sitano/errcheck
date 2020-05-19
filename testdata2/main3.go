package testdata2

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

type E struct{}

func (e E) Error() string {
	panic("implement me")
}

func main() {
	_, err := os.Create("/tmp/blah")
	_, err = os.Create("/tmp/blah") // lost update
	{
		err = errors.New("b") // lost update
	}
	{
		var a, b = 1, 2
		var c, d int = 1, 2
		var e, f int
		var g = E{}.Error() // error dependency
		var z0 = len(g)
		var g0 = (struct {a int}{a: 1})
		var z1 = g0.a
		var x error = E{} // error
		var y = E{} // implements error
		var z = e + 1 + 2 + a + b + c + d + e + f + len(g) + g0.a + z1 + z0 // error dependency
		err = x // write
		err = y // lost update
		err = errors.New(strconv.Itoa(z))
	}
	{
		y := error(E{})
		err = y
	}
	if true {
		err = errors.New("1")
	} else {
		err = errors.New("2" + err.Error())
	}
	if err != nil {
		fmt.Println(123)
	}
	if a := len(E{}.Error()); a > 0 { // functional dependency on error
		err = errors.New(strconv.Itoa(a)) // lost update
	}
}
