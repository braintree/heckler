# Testing

Heckler has two types of tests at present. First, standard Go tests
which cover some of the code base, though many more could be added.
Second, an integration test with a sample repo.

## Go Tests

    $ make docker-test

## Integration Test

docker-compose is a prerequisite for running these tests, and we also highly
recommend using tmux as well, since we will be running lots of foreground
processes in our shell.

1.  [Create a GitHub app for testing
    heckler](https://docs.github.com/en/apps/creating-github-apps/creating-github-apps/creating-a-github-app).

    1.  After the app is created, open its configuration and generate a private
        key in `Private keys` section.
    2.  Copy the private RSA key to `github-private-key.pem` in the root folder
        of this repo.
    3.  Update [`hecklerd_conf.yaml`][] with the following values, now that you
        have them:

            * `github_app_slug`
            * `github_app_email`
            * `github_app_id`

2.  [Create the test GitHub
    repo](https://docs.github.com/en/get-started/quickstart/create-a-repo),
    probably under your personal org, but that is up to you. (Do _not_,
    however, initialize it with a first commit!)

    1.  [Install the GitHub application you created in Step 1 to your
        repo.](https://docs.github.com/en/apps/maintaining-github-apps/installing-github-apps)
        (If you created your repo in an org you are not an admin of, you will
        need to contact your admin to get the installation approved.)

    2.  Update [`hecklerd_conf.yaml`][] with the following values, now that you
        have them:

            * `github_app_install_id`
            * `repo`
            * `repo_owner` (i.e. the github org, which, if using a personal
                repo, is just going to be your username)
            * `repo_branch` (if not `main`)
            * `github_domain` (if doing this on your company's GitHub
                Enterprise installation instead of public GitHub)

    3.  (If using a personal repo and working with others) [Add any teammates
        you want to test with as repo
        collaborators.](https://docs.github.com/en/account-and-profile/setting-up-and-managing-your-personal-account-on-github/managing-access-to-your-personal-repositories/inviting-collaborators-to-a-personal-repository)

    4.  Add your GitHub username (plus any collaborators) as admins in
        [`hecklerd_conf.yaml`][]:

        ```yaml
        admin_owners:
          - "@your_username"
          - "@your_collaborators_username"
        ```

    5.  Use the [`make_repo`][] script to init your test repo and create some
        commits and release tags.

        ```sh
        ./make-repo -u <existing sample github url>
        ```

3.  Start the docker containers:

    1.  Start our docker-compose setup, which creates a container that
        represents the management host (it'll run `hecklerd`), plus three
        containers that represent members of your server fleet.

        ```sh
        cd docker-compose
        make run
        ```

    2.  Set up your `ssh_config` to make the rest of this process easier.

        ```sh
        make ssh-config
        ```

4.  Start the `hecklerd` and `rizzod` processes in the containers:

    ```sh
    ssh heckler 'hecklerd -configfile=/heckler/docs/sample-config/hecklerd_conf.yaml'
    ssh statler 'rizzod -configfile=/heckler/docs/sample-config/rizzod_conf.yaml'
    ssh waldorf 'rizzod -configfile=/heckler/docs/sample-config/rizzod_conf.yaml'
    ssh fozzie 'rizzod -configfile=/heckler/docs/sample-config/rizzod_conf.yaml'
    ```

5.  Force a puppet apply of the state from tag `v1` from your test repo:

    ```sh
    ssh heckler 'heckler -rev v1 -force'
    ```

6.  Watch heckler noop & apply the remaining commits.


[`hecklerd_conf.yaml`]: /docs/sample-configs/hecklerd_conf.yaml
