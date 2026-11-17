package io.github.rclsilver.home_genie.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertSwipeTest {
    @Test
    fun `release at half width acknowledges`() {
        assertTrue(shouldAcknowledgeAlertSwipe(-200f, 400f))
        assertTrue(shouldAcknowledgeAlertSwipe(-400f, 400f))
    }

    @Test
    fun `short or rightward drags do nothing`() {
        assertFalse(shouldAcknowledgeAlertSwipe(-199f, 400f))
        assertFalse(shouldAcknowledgeAlertSwipe(0f, 400f))
        assertFalse(shouldAcknowledgeAlertSwipe(300f, 400f))
    }

    @Test
    fun `unmeasured rows cannot acknowledge`() {
        assertFalse(shouldAcknowledgeAlertSwipe(-200f, 0f))
        assertFalse(shouldAcknowledgeAlertSwipe(-200f, -400f))
    }
}
