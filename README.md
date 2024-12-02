# cardano-up

## Installation

### Install latest release (recommended)

You can download the latest releases from the [Releases page](https://github.com/blinklabs-io/cardano-up/releases).
Place the downloaded binary in `/usr/local/bin`, `~/.local/bin`, or some other convenient location and make sure
that location has been added to your `$PATH`. Our recommendation is to use `~/.local/bin` as that is where this
tool will install wrapper scripts.

NOTE: On MacOS, you will need to allow `/` to be used by Docker Desktop

## Basic usage

### List available packages

```
cardano-up list-available
```

### Install a package and interact with it

Add `~/.local/bin` to your `$PATH` by adding the following to your shell RC/profile to make any
commands/scripts installed readily available

```
export PATH=~/.local/bin:$PATH
```

Install cardano-node

```
cardano-up install cardano-node
```

You can also add any env vars exported by the installed packages to your env by adding the following to your shell RC/profile:

```
eval $(cardano-up context env)
```

You should now be able to run `cardano-cli` normally.

```
cardano-cli query tip --testnet-magic 2
```

### Uninstall a package

```
cardano-up uninstall cardano-node
```

### Enabling shell auto-complete

Enable for current session:

```
source <(cardano-up completion bash)
```

Enable for all future sessions:

```
cardano-up completion bash > /etc/bash_completion.d/cardano-up
```

## Contexts

Contexts are used to allow you to install multiple copies of the same package with different network configurations side by side. They allow you to do things
such as running a `preprod` and `mainnet` Cardano node on the same machine, or even have multiple `preview` Cardano node instances running different versions
of the node.

Commands such as `install`, `uninstall`, and `list` work in the active context. You can use the `context` command to change the active context or manage available contexts.

## Command reference

The `cardano-up` command consists of multiple subcommands. You can list all subcommands by running `cardano-up` with no arguments or with the `--help` option.

```
$ cardano-up
Usage:
  cardano-up [command]

Available Commands:
  completion     Generate the autocompletion script for the specified shell
  context        Manage the current context
  down           Stops all Docker containers
  help           Help about any command
  info           Show info for an installed package
  install        Install package
  list           List installed packages
  list-available List available packages
  logs           Show logs for an installed package
  uninstall      Uninstall package
  up             Starts all Docker containers
  update         Update the package registry cache
  upgrade        Upgrade package
  validate       Validate package file(s) in the given directory
  version        Displays the version

Flags:
  -D, --debug   enable debug logging
  -h, --help    help for cardano-up

Use "cardano-up [command] --help" for more information about a command.
```

### `completion`

The `completion` subcommand generates shell auto-completion configuration for various supported shells. Run `completion help <shell>` for more information on installing completion support for your shell.

### `context`

The `context` subcommand manages contexts. It has subcommands of its own for the various context-related functions.

#### `context create`

Create a new context with a given name, optionally specifying a description and a Cardano network

#### `context delete`

Delete the context with the given name, if it exists

#### `context env`

Output environment variables for the active context

#### `context list`

Lists the available contexts

#### `context select`

Sets the active context to the given context name

### `down`

Stops all running services for packages in the active context

### `help`

Displays usage information for commands and subcommands

### `info`

Shows information for an installed package, including the name, version, context name, any post-install notes, etc.

### `install`

Installs the specified package, optionally setting the network for the active context

### `list`

Lists installed packages in the active context, or all contexts with `-A`

### `list-available`

List all packages available for install

### `logs`

Displays logs from a running service for the specified package in the active context

### `uninstall`

Uninstalls the specified package in the active context

### `up`

Starts all services for packages in the active context

### `update`

Force a refresh of the package registry cache

### `upgrade`

Upgrade the specified package

### `validate`

Validates packages defined in specified path

### `version`

Displays the version

## Development

### Install from source

Before starting, make sure that you have at least Go 1.21 installed locally. Run the following
to download the latest source code and build.

```
go install github.com/blinklabs-io/cardano-up/cmd/cardano-up@main
```

Once that completes, you should have a `cardano-up` binary in `~/go/bin`.

```
$ ls -lh ~/go/bin/cardano-up
-rwxrwxr-x 1 agaffney agaffney 16M Mar 16 08:13 /home/agaffney/go/bin/cardano-up
```

You may need to add a line like the following to your shell RC/profile to update your PATH
to be able to find the binary.

```
export PATH=~/go/bin:$PATH
```

### Compile from source

There is a Makefile (you will need `make` installed) which you can invoke.

```bash
make
```

This will create a `cardano-up` binary in the repository root.

### Creating and maintaining packages

Packages and their versions are defined under `packages/` in this repo. Each separate package name has its own subdirectory,
and each version of a particular package is defined in a separate file under that subdirectory. For example, package `foo` with
version `1.2.3` would live in `packages/foo/foo-1.2.3.yaml`.

#### Testing local changes to packages

The remote package repo will be used by default when running `cardano-up`. To instead use the package files in a local directory, you
can run it like:

```bash
REGISTRY_DIR=packages/ cardano-up ...
```

#### Validating package files

There is a built-in subcommand for validating package files. It will be run automatically for a PR, but you can also run it manually.

```bash
cardano-up validate packages/
```

#### Templating

Package manifest files are evaluated as a Go template before being parsed as YAML. The following values are available for use in templates.

| Name | Description |
| --- | --- |
| `.Package` | |
| `.Package.Name` | Full package name including the version |
| `.Package.ShortName` | Package name |
| `.Package.Version` | Package version |
| `.Package.Options` | Provided package options |
| `.Paths` | |
| `.Paths.CacheDir` | Cache dir for package |
| `.Paths.ContextDir` | Context dir for package |
| `.Paths.DataDir` | Data dir for package |
| `.Ports` | Container port mappings |

#### Package manifest format

The package manifest format is a YAML file with the following fields:

| Field | Required | Description |
| --- | :---: | --- |
| `name` | x | Package name. This must match the prefix of the package manifest filename and the parent directory name |
| `version` | x | Package version |
| `description` | | Package description |
| `preInstallScript` | | Arbitrary command that will be run before the package is installed |
| `postInstallScript` | | Arbitrary command that will be run after the package is installed |
| `preUninstallScript` | | Arbitrary command that will be run before the package is uninstalled |
| `postUninstallScript` | | Arbitrary command that will be run after the package is uninstalled |
| `installSteps` | | Steps to install package |
| `dependencies` | | Dependencies for the package |
| `tags` | | Tags for the package |
| `options` | | Install-time options |
| `outputs` | | Package outputs |

##### `installSteps`

The install steps for a package consist of a list of resources to manage. They are applied in order on install and reverse order on uninstall.

Each install step may contain a condition that will make it's evaluation optional. A condition will be implicitly wrapped in `{{ if ` and ` }}True{{ else }}False{{ end }}` and evaluated by the templating engine.

###### `docker`

The `docker` install step type manages a Docker container.

Example:

```yaml
installSteps:
  - docker:
      containerName: nginx
      image: nginx
```

| Field | Required | Description |
| --- | :---: | --- |
| `containerName` | x | Name of the container to create. This will be automatically prefixed by the package name |
| `image` | x | Docker image to use for container |
| `env` | | Environment variables for container (expects a map) |
| `command` | | Override container command (expects a list) |
| `args` | | Override container args (expects a list) |
| `binds` | | Volume binds in the Docker `-v` flag format (expects a list) |
| `ports` | | Ports to map in the Docker `-p` flag format (expects a list). NOTE: assigning a static port mapping may cause conflicts |
| `pullOnly` | | Only pull the image to pre-fetch it (expects a bool, defaults to creating container) |

###### `file`

The `file` install step type manages a file.

Example:

```yaml
installSteps:
  - file:
      filename: my-file
      source: my-source-file
```

| Field | Required | Description |
| --- | :---: | --- |
| `filename` | x | Name of destination file. This will be created within the package's data directory |
| `source` | | Path to source file. This should be a relative path within the package manifest directory. This takes precedence over `content` if both are provided |
| `content` | | Inline content for destination file |
| `mode` | | Octal file mode for destination file |
| `binary` | | Whether this file is an executable file for the package (expects bool, defaults to `false`) |

##### `dependencies`

Dependencies for a package are specified in the following format. At minimum they contain a package name. They may optionally contain a list of required package
options and version range(s).

Examples:

Package `foo` with at least version `1.0.2`

```
foo >= 1.0.2
```

Package `foo` with at least version `1.0.2` but less than `2.0.0`

```
foo < 2.0.0, >= 1.0.2
```

Package `bar` with at least version `3.0.0`, option `optA` turned on, and option `optB` turned off

```
bar[optA,-optB] >= 3.0.0
```

##### `tags`

The tags for a package should be a list of arbitrary string values corresponding to the supported platforms and architectures. They should be one or more of:

* `docker`
* `linux`
* `darwin`
* `amd64`
* `arm`

##### `options`

The options for a package allow defining optional feature flags. The value of these flags is available to templates in the package manifest.

Example:

```yaml
options:
  - name: foo
    description: Option foo
    default: false
```

This option could then be referenced as `.Package.Options.foo` in package templates.

| Field | Required | Description |
| --- | :---: | --- |
| `name` | x | Name of the option |
| `description` | | Description of the option |
| `default` | | Default value for option (defaults to `false`) |

##### `outputs`

The outputs defined in a package will be translated into environment variables for the user to consume.

Example:

```yaml
  - name: socket_path
    description: Path to the Cardano Node UNIX socket
    value: '{{ .Paths.ContextDir }}/node-ipc/node.socket'
```

When used in package `cardano-node`, this will generate an env var named `CARDANO_NODE_SOCKET_PATH` with a path inside the package's data directory.

| Field | Required | Description |
| --- | :---: | --- |
| `name` | x | Name of the output. This will have the package name automatically prepended and be made upper case |
| `description` | | Description of the output |
| `value` | x | Template that will be evaluated to generate the static output value |
