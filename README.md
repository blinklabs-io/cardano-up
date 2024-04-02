# cardano-up

## Installation

### Install latest release

You can download the latest releases from the [Releases page](https://github.com/blinklabs-io/cardano-up/releases).
Place the downloaded binary in `/usr/local/bin`, `~/.local/bin`, or some other convenient location and make sure
that location has been added to your `$PATH`. 

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

## Basic usage

### List available packages

```
cardano-up list-available
```

### Install a package and interact with it

```
cardano-up install cardano-node
```

Add `~/.local/bin` to your `$PATH` by adding the following to your shell RC/profile to make any
commands/scripts installed readily available

```
export PATH=~/.local/bin:$PATH
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
