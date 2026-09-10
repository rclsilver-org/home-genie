# Android SDK for developing the application, meant to be used in an
# ephemeral shell:
#
#   nix-shell nix/android-sdk.nix
#   cd android && gradle assembleDebug
#
# Nothing is installed on the system: the SDK is realised in /nix/store and
# the variables only exist inside the shell.
#
# Versions follow android/gradle/libs.versions.toml (compileSdk 36, AGP
# 8.10.x). `buildToolsVersion` must stay in step with the one pinned in
# android/app/build.gradle.kts.
#
# `emulator` is deliberately absent: everyday testing happens on a real
# phone, and an AOSP emulator would say nothing about a manufacturer's skin.
# See nix/android-emulator.nix for the shell that carries one.

{ pkgs ? import <nixpkgs> {
    config.android_sdk.accept_license = true;
    config.allowUnfree = true;
  }
}:

let
  # Single source of truth: the composed SDK and the aapt2 path forced
  # below must speak of the same version.
  buildToolsVersion = "36.0.0";
  platformVersion = "36";

  androidComposition = pkgs.androidenv.composeAndroidPackages {
    platformVersions = [ platformVersion ];
    buildToolsVersions = [ buildToolsVersion ];
    includeEmulator = false;
    includeSystemImages = false;
    includeSources = false;
  };

  sdk = "${androidComposition.androidsdk}/libexec/android-sdk";
in
pkgs.mkShell {
  buildInputs = [
    androidComposition.androidsdk
    pkgs.jdk17
    pkgs.gradle
    pkgs.android-tools # adb
  ];

  ANDROID_HOME = sdk;
  ANDROID_SDK_ROOT = sdk;
  JAVA_HOME = "${pkgs.jdk17}";

  # The aapt2 AGP downloads from Maven is a generic, unpatchelfed binary:
  # its dynamic loader does not exist on NixOS and the AAPT2 daemon fails
  # to start. The override forcing the SDK's own is applied by the
  # Makefile's `apk` target, not here: Gradle forks a single-use daemon to
  # honour the jvmargs from gradle.properties, and that daemon does not
  # inherit properties passed through GRADLE_OPTS.

  # Banner on stderr, for the same reason as in shell.nix: on stdout it
  # pollutes the output of any `nix-shell --run` read through a pipe.
  shellHook = ''
    {
      echo "Android SDK  : $ANDROID_HOME"
      echo "build-tools  : ${buildToolsVersion}"
      echo "Wi-Fi debug  : adb pair <ip>:<port> then adb connect <ip>:5555"
    } >&2
  '';
}
