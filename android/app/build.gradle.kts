plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

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

        versionCode = 1
        versionName = project.findProperty("appVersionName")?.toString() ?: "dev"
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
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
}
