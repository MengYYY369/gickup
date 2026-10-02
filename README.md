<h1 align="center">
    <img src="https://github.com/cooperspencer/gickup/blob/main/gickup.png" style="width: 20%;" alt="logo">
    <br/>
    GICKUP
</h1>

<h4 align="center">
    Backup your Git repositories with ease.
</h4>


<p align="center">
    <strong>
        <a href="https://cooperspencer.github.io/gickup-documentation/" target="_blank">Website</a>
        •
        <a href="https://github.com/cooperspencer/gickup/">GitHub</a>
        •
        <a href="https://cooperspencer.github.io/gickup-documentation/" target="_blank">Docs</a>
    </strong>
</p>

<p align="center">
    <a href="https://github.com/cooperspencer/gickup/actions/workflows/docker.yml">
        <img alt="Build and Publish" src="https://github.com/cooperspencer/gickup/actions/workflows/docker.yml/badge.svg">
    </a>
</p>



## What is GICKUP?
Gickup is a tool that allows you to clone/mirror repositories from one host to another.
This is useful if you want to have a backup of your repositories on another host or on a local server.


### Supported Source and Destinations
You can clone/mirror repositories from:
- Github
- Gitlab
- Codeberg (Forgejo)
- Gitea
- Gogs
- Bitbucket
- OneDev
- Sourcehut
- Opengist
- Any

You can clone/mirror repositories to:
- Github
- Gitlab
- Codeberg (Forgejo)
- Gitea
- Gogs
- OneDev
- Sourcehut
- Radicle
- Local
- S3


If your hoster is not listed, feel free to open an issue and I will add it.



## How to make a configuration file
[Here is an example](https://github.com/cooperspencer/gickup/blob/main/conf.example.yml)

## How to run the binary version
`./gickup path-to-conf.yml`

## Run the configuration WebUI
Gickup can serve a local configuration editor for the YAML files in a directory:

```bash
./gickup webui --config-dir /path/to/configs --port 6175
```

- listens on `127.0.0.1` only and prints the URL; no login is required
- manages the `*.yml` and `*.yaml` files directly inside the directory (no recursion)
- loads, validates, edits and saves configurations while preserving comments, key order and unknown fields
- includes a file workspace (create blank, create from `conf.example.yml`, import, rename, copy, delete to a trash folder) and a per-file backup with restore
- sensitive values such as `token`, `password` and `secret` are masked by default and can be revealed on demand
- `--config-dir` defaults to the current working directory, `--port` defaults to `6175`

## Trigger backups with a GitHub webhook
Instead of waiting for the next cron run, Gickup can stay running and synchronize the matching GitHub source whenever GitHub sends a push webhook:

```yaml
webhook:
  enabled: true
  listen_addr: ":8081"
  path: /webhooks/github
  secret: your-webhook-secret
  queue_capacity: 32
```

- every push delivery must be signed with `X-Hub-Signature-256`; a non-empty secret is required
- `ping` events are acknowledged without starting a synchronization
- accepted pushes are processed one at a time through a bounded FIFO queue; a full queue returns HTTP 503
- `GET /healthz` reports whether the listener is up
- cron scheduling and webhook mode can run together

## How to run the Docker image
```bash
mkdir gickup
wget https://raw.githubusercontent.com/cooperspencer/gickup/main/docker-compose.yml
nano conf.yml # Make your config here
docker-compose up
```
## Compile the binary version
The binary embeds the WebUI, so build the frontend first:

```bash
make build   # runs npm ci && npm run build inside webui/, then go build
```

`go build .` alone works only after `webui/dist` has been generated.

## Compile the Docker Image
```bash
git clone https://github.com/cooperspencer/gickup.git
cd gickup
nano docker-compose.yml # Uncomment the Build
nano conf.yml # Make your config here
docker-compose build
docker-compose up
```

## Questions?
If anything is unclear or you have a great idea for the project, feel free to open a discussion about it.
https://github.com/cooperspencer/gickup/discussions

## Distribution Packages
|Distribution|Package|Maintainer|
|---|---|---|
|Arch|[gickup](https://aur.archlinux.org/packages/gickup/)|[me](https://github.com/cooperspencer)|
|Homebrew|[gickup](https://formulae.brew.sh/formula/gickup#default)||
|Fedora|[gickup](https://copr.fedorainfracloud.org/coprs/frostyx/gickup/)|[FrostyX](https://github.com/FrostyX)|
|Scoop|[gickup](https://scoop.sh/#/apps?q=gickup&id=493e856707843828b4491004edfb6ddedc4bd1e1)||
|Winget|[gickup](https://github.com/microsoft/winget-pkgs/tree/master/manifests/c/cooperspencer/gickup)||

## Issues
The mirroring to Gitlab doesn't work, or at least I can't test it properly because I have no access to a Gitlab EE instance.

## Future Ideas
- Additional VCS
  - [GitBucket](https://gitbucket.github.io/)
- Add minio as a destination
