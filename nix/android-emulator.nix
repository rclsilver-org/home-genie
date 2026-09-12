# Android SDK *with* the emulator, for automated test campaigns.
#
#   nix-shell nix/android-emulator.nix --run 'make emulator'
#
# Deliberately separate from nix/android-sdk.nix: the system image weighs close
# to two gigabytes and a daily compile does not need it. The development shell
# stays light; this one is only for test runs.
#
# The emulator does not replace the phone: it is AOSP without a manufacturer's
# skin, so it says nothing about battery arbitration, this project's first
# risk. It does what the phone does badly — replay a whole journey, hands-free
# and repeatably.
{ pkgs ? import <nixpkgs> {
    config.android_sdk.accept_license = true;
    config.allowUnfree = true;
  }
}:

let
  buildToolsVersion = "36.0.0";
  platformVersion = "36";

  androidComposition = pkgs.androidenv.composeAndroidPackages {
    platformVersions = [ platformVersion ];
    buildToolsVersions = [ buildToolsVersion ];
    includeEmulator = true;
    includeSystemImages = true;
    # google_apis rather than google_apis_playstore: the application is
    # deployed with `adb install`, and a Play Store image locks itself down on
    # many points (no root, unmodifiable image) without buying anything here.
    systemImageTypes = [ "google_apis" ];
    abiVersions = [ "x86_64" ];
    includeSources = false;
  };

  sdk = "${androidComposition.androidsdk}/libexec/android-sdk";
in
pkgs.mkShell {
  buildInputs = [
    androidComposition.androidsdk
    pkgs.jdk17
    pkgs.gradle
  ];

  ANDROID_HOME = sdk;
  ANDROID_SDK_ROOT = sdk;
  JAVA_HOME = pkgs.jdk17;

  shellHook = ''
    export PATH="${sdk}/emulator:${sdk}/platform-tools:${sdk}/cmdline-tools/latest/bin:$PATH"
    # Banner on stderr: read through a pipe, it would pollute the output of
    # the commands chained inside this shell.
    echo "SDK + emulator: ${sdk}" >&2
  '' ;
}
