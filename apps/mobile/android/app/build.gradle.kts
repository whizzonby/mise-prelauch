import java.util.Properties

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// Release signing comes only from android/key.properties (never committed;
// see docs/launch/ANDROID_RELEASE_CHECKLIST.md). Without it, release builds
// are unsigned and cannot be uploaded or installed.
val keystoreProperties = Properties().apply {
    val f = rootProject.file("key.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}

android {
    // Proposed identifier, pending the owner's approval (SPRINT_10_AUDIT.md §9).
    namespace = "tt.mise.customer"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        applicationId = "tt.mise.customer"
        minSdk = maxOf(flutter.minSdkVersion, 24)
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    // dev and staging install beside production with their own id, name and
    // link scheme, so test builds can never be mistaken for the real app.
    flavorDimensions += "env"
    productFlavors {
        create("dev") {
            dimension = "env"
            applicationIdSuffix = ".dev"
            versionNameSuffix = "-dev"
            resValue("string", "app_name", "Mise Dev")
            manifestPlaceholders["appScheme"] = "mise-local"
            manifestPlaceholders["networkConfig"] = "@xml/network_security_config_dev"
        }
        create("staging") {
            dimension = "env"
            applicationIdSuffix = ".staging"
            versionNameSuffix = "-staging"
            resValue("string", "app_name", "Mise Beta")
            manifestPlaceholders["appScheme"] = "mise-staging"
            manifestPlaceholders["networkConfig"] = "@xml/network_security_config"
        }
        create("prod") {
            dimension = "env"
            resValue("string", "app_name", "Mise")
            manifestPlaceholders["appScheme"] = "mise"
            manifestPlaceholders["networkConfig"] = "@xml/network_security_config"
        }
    }

    signingConfigs {
        if (keystoreProperties.containsKey("storeFile")) {
            create("release") {
                storeFile = file(keystoreProperties["storeFile"] as String)
                storePassword = keystoreProperties["storePassword"] as String
                keyAlias = keystoreProperties["keyAlias"] as String
                keyPassword = keystoreProperties["keyPassword"] as String
            }
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release")
            isMinifyEnabled = true
            isShrinkResources = true
        }
    }
}

flutter {
    source = "../.."
}
