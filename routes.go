package main

func (s *server) routes() {
	s.router.HandleFunc("/api/v1/spatch/{drbdversion}", s.spatchCreate()).Methods("POST")
}
