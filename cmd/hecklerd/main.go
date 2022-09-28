package main

import (
	hecklerd "github.com/braintree/heckler/apps/hecklerd"
	"log"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	hApp, hAppError := hecklerd.NewHecklerdApp()
	if hAppError == nil {
		log.Println("got hApp", hApp)
		hApp.Run()
	} else {
		log.Println("hAppError::", hAppError)
	}

}
