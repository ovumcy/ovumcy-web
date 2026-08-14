none

CI only: the Go job now fails on a function nothing can reach, via a pinned
`deadcode -test` run over the same five package trees the other Go steps use.
No product code.
