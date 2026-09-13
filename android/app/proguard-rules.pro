# R8 is on for release builds, and two of this application's dependencies rely
# on things R8 cannot see.

# kotlinx.serialization resolves serializers through generated companions that
# nothing references directly. Its artifact ships consumer rules, but the
# @Serializable classes themselves are ours: keep their generated serializers,
# or every payload from the server fails to decode at runtime — and only at
# runtime, which is exactly the failure a release build must not introduce.
-keepclassmembers class io.github.rclsilver.home_genie.** {
    *** Companion;
}
-keepclasseswithmembers class io.github.rclsilver.home_genie.** {
    kotlinx.serialization.KSerializer serializer(...);
}
-keep,includedescriptorclasses class io.github.rclsilver.home_genie.**$$serializer { *; }

# The service, the receivers and the activity are named in the manifest, not
# called from code. AGP keeps manifest-declared components, but the actions
# they carry as extras are string constants — keep the classes whole rather
# than rely on that.
-keep class io.github.rclsilver.home_genie.service.** { *; }
