## Create a puppet repo

Create a github repo with all your puppet files in it.

Add the heckler vendor directory from manifests/vendor/heckler to your basemodulespath somewhere

Add `,heckler` to `reports =`.

Add a CODEOWNERS to your repo.
 
You could also run `./make-repo -u git@github.com/your_username/your_new_puppet.git` in the repo's root directory.  That has all the puppet config, a CODEOWNERS and some example hosts. IT WILL OVERWRITE THE ABOVE REPO, DON'T USE AN OLD REPO FOR THAT. 

## Register new GitHub App

Head over to https://braintree.github.io/heckler/create_github_app.html and click the button.

Name the app, it must be unique. You will be redirected back to https://braintree.github.io/heckler/redirect.html, but with a secret code as a parameter.  Fill in your GITHUB_TOKEN, created through https://github.com/settings/tokens.  It can have short validity, it's not used after the next step.  Click the button and it will complete the curl command.  Run that in a location you keep secret information (encrypted, etc), as it contains the PRIVATE KEY for your app.

### Configure Heckler

pull out the private key

```
jq '.pem' -cr settings.json > ./github-hecklerd.pem
```

or wherever you're keeping super secret info.  Maybe `/etc/heckler` or something.

Get the app id:
```
jq '.id' settings.json
```
and put that in the heckler config (copy the sample config and put it in /etc/heckler).

Put the name of your repo as `repo:`, your username as `repo_owner:`, leaving the yaml anchor.  Put the name of your main branch as `repo_branch`.  Set the email to your email.

go to the HTML_URL:

```
jq '.html_url' -cr settings.json
```

Click "Install" as the user that owns the puppet repo.  Choose "only select repositories" and select your puppet repo.

The page you end on is in your profile, under application and the name that you gave heckler (like heckler-dev).  This page will end in a number.  That number is the "github_app_install_id".  If your URL were:

https://github.com/settings/installations/999999

your install_id would be 999999.  Put that in the heckler config.

Go back to the "App settings" via the link at the top, or click on the HTML_URL again, as that's the same thing.

Add the IP that heckler will reach out to the GitHub API with at the very bottom in the "IP allow list" section.


## Run heckler

```
./hecklerd
```

or on a usr_merged linux instance:

```
docker build -t heckler-scratch -f Dockerfile.heckler; docker run -it --rm -v $PWD/github-hecklerd.pem:/github-hecklerd.pem -v /usr:/usr:ro heckler-scratch
```
