package testdata2

import (
	"errors"
	"fmt"
	"os"
)

type E struct{}

func (e E) Error() string {
	panic("implement me")
}

func main() {
	_, err := os.Create("/tmp/blah")
	_, err = os.Create("/tmp/blah")
	{
		err = errors.New("b")
	}
	{
		var x error = E{}
		err = x
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
}
