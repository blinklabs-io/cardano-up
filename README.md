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

### List supported commands

You can run `cardano-up` to get a list of supported commands.

```
$ cardano-up
Usage:
  cardano-up [command]

Available Commands:
...
Use "cardano-up [command] --help" for more information about a command.
```

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
