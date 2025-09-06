# Solbuild Release Engineering Checklist

Perform the following steps after tagging the Solbuild release in order to verify that it introduces no major regressions:

- Build and install a local update of the solbuild package corresponding to the newly tagged release:
  ```
  gotosoluspkgs
  gotopkg solbuild
  yupdate X.Y.Z https://github.com/getsolus/solbuild/archive/refs/tags/vX.Y.Z.tar.gz
  go-task
  sudo eopkg it *.eopkg
  ```

- Ensure the version number is correct:
  ```
  solbuild version
  ```
  Verify that this shows the correct version number.

- Delete cache and existing solbuild images:
  ```
  sudo solbuild dc -dai
  ```

- Initialise unstable profile:
  ```
  sudo solbuild init -du
  ```
  Verify that this uses the unstable profile and shows debug output.

- Initialise Shannon profile:
  ```
  sudo solbuild init -nup main-x86_64
  ```
  Verify that this shows non-colored output.

- Delete local repo:
  ```
  sudo rm -rvf /var/lib/solbuild/local/*
  ```
- Build `zlib-ng` against stable without colored output:
  ```
  gotosoluspkgs
  gotopkg zlib-ng
  sudo solbuild build -n -p main-x86_64 > zlib-stable.log
  ```
  Verify that this shows non-colored output.

- Chroot into the build environment:
  ```
  sudo solbuild chroot -p main-x86_64 -d -n
  ```
- Build zlib against unstable and copy to local repo:
  ```
  sudo solbuild build -d
  sudo cp -v zlib*.eopkg /var/lib/solbuild/local/
  ```

- Index the local repo:
  ```
  sudo solbuild index -d /var/lib/solbuild/local/
  ```

- Build the recommended test set of packages, which exercises problematic SourceForget URIs and git submodule functionality:
  ```
  for p in android-tools giflib glew zsh
  do
    gotopkg $p
    sudo solbuild build >> /tmp/builds.log
  done
  ```

If all of the above completes successfully, proceed to `go-task publish` the Solbuild update.
