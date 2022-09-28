package main

import (
	rizzod "github.com/braintree/heckler/apps/rizzod"
	"log"
)

func main() {
	rizzodApp, rizzodAppError := rizzod.NewRizzodApp()
	if rizzodAppError == nil {
		log.Println("got hApp", rizzodApp)
		rizzodApp.Run()
	} else {
		log.Println("rizzodAppError::", rizzodAppError)
	}

}
