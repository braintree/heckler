# Testing

Heckler has two types of tests at present. First, standard Go tests
which cover some of the code base, though many more could be added.
Second, an integration test with a sample repo.

## Go Tests

    $ make docker-test

## Integration Test

1.  Create the test GitHub repo

        $ ./make-repo -u <existing sample github url>`

2.  Start up docker containers

        $ cd docker-compose
        $ make run

3.  Update ssh config

        Match all
        Include <src dir>/docker-compose/node/ssh_configs/ssh_config

4.  SSH to nodes

        ssh heckler 'cd /heckler; ./hecklerd'
        ssh statler 'cd /heckler; ./rizzod'
        ssh waldorf 'cd /heckler; ./rizzod'
        ssh fozzie 'cd /heckler; ./rizzod'

5.  Force apply tag `v1`

        ssh heckler 'cd /heckler; ./heckler -rev v1 -force'

6.  Watch heckler noop & apply the remaining commits
