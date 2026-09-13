plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// Android compares versionCode — never versionName — to decide that one build
// supersedes another. It is derived from the name Gradle is already given
// rather than passed as a second property: two values that can disagree are
// worse than one that has to be parsed.
//
// The commit distance of an off-tag build is deliberately dropped. Folding it
// in would make 0.2.0-2 outrank the 0.2.1 that comes next, which is the very
// regression this exists to prevent.
fun versionCodeOf(name: String): Int {
    val (major, minor, patch) =
        Regex("""^(\d+)\.(\d+)\.(\d+)""").find(name)?.destructured ?: return 1
    // 0.100.0 and 1.0.0 would land on the same code, and a collision stays
    // invisible until updates silently stop arriving.
    require(minor.toInt() < 100 && patch.toInt() < 100) {
        "version $name does not fit the versionCode scheme"
    }
    return major.toInt() * 10000 + minor.toInt() * 100 + patch.toInt()
}

val appVersion = project.findProperty("appVersionName")?.toString() ?: "dev"

android {
    namespace = "io.github.rclsilver.home_genie"
    compileSdk = 36

    // Pinned on purpose: AGP otherwise resolves its own default (34.0.0) and
    // tries to install it into the SDK, which fails because androidenv keeps
    // the SDK read-only in the nix store. Must match the buildToolsVersions
    // listed in nix/android-sdk.nix.
    buildToolsVersion = "36.0.0"

    defaultConfig {
        applicationId = "io.github.rclsilver.home_genie"
        minSdk = 29

        // The test phone runs Android 16. Targeting it rather than 35 means the
        // foreground service is measured under the behaviour it will actually
        // run under, not under a compatibility mode we would lose later.
        targetSdk = 36

        versionCode = versionCodeOf(appVersion)
        versionName = appVersion
    }

    // Release signing comes from the environment, never from a file in the
    // tree: the repository is public. CI fills these from secrets; a local
    // `make apk` leaves them unset and simply builds unsigned.
    //
    // The key must stay the same forever. Android identifies an application by
    // its signature, so a new key is a new application: the phone has to
    // uninstall, which drops the device token and the replay cursor. That is
    // the one thing this project cannot afford to do on every release.
    val keystorePath = System.getenv("ANDROID_KEYSTORE_PATH")
    signingConfigs {
        if (keystorePath != null) {
            create("release") {
                storeFile = file(keystorePath)
                storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ANDROID_KEY_ALIAS")
                keyPassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
            if (keystorePath != null) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
    }
}

dependencies {
    implementation(libs.androidx.browser)
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.service)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.datastore.preferences)
    implementation(libs.okhttp)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.androidx.work.runtime.ktx)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)

    debugImplementation(libs.androidx.compose.ui.tooling)

    // Plain JVM tests, no Robolectric: what is worth testing here is decision
    // logic, which is kept free of Android types precisely so it can be.
    testImplementation(libs.junit)
}
