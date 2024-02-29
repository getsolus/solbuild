Perform the following steps after tagging the Solbuild release in order to verify that it introduces no major regressions:

- Build and install a local update of the solbuild package:
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
  Verify that this uses the unstable profile.

- Initialise Shannon profile:
  ```
  sudo solbuild init -nup main-x86_64
  ```
  Verify that this shows non-colored output.

- Delete local repo:
  ```
  sudo rm -rvf /var/lib/solbuild/local/*
  ```
- Build `zlib` against stable without colored output:
  ```
  gotosoluspkgs
  gotopkg zlib
  sudo solbuild build -n -p main-x86_64 > zlib-stable.log
  ```
  Verify that this shows non-colored output.

- Chroot into the build environment:
  ```
  sudo solbuild -d -n chroot -p main-x86_64
  ```
- Build zlib against unstable and copy to local repo:
  ```
  sudo solbuild build -d
  sudo cp zlib*.eopkg /var/lib/solbuild/local/
  ```

- Index the local repo:
  ```
  sudo solbuild index -d /var/lib/solbuild/local/
  ```

- Build the test set of packages:
  ```
  for p in android-tools giflib glew zsh
  do
    sudo solbuild build >> /tmp/builds.log
  done
  ```

If all of the above completes successfully, proceed to `go-task publish` the Solbuild update.
