package credentials

import "golang.org/x/crypto/bcrypt"

func init() { bcryptCost = bcrypt.MinCost }
