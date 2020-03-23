package controller

import (
	"github.com/gympass/ecr-operator/pkg/controller/ecr"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, ecr.Add)
}
