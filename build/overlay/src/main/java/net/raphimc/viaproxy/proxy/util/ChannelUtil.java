/*
 * This file is part of ViaProxy - https://github.com/RaphiMC/ViaProxy
 * Copyright (C) 2021-2026 RK_01/RaphiMC and contributors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */
package net.raphimc.viaproxy.proxy.util;

import io.netty.channel.Channel;
import io.netty.channel.EventLoop;
import io.netty.util.AttributeKey;

import java.util.ArrayDeque;
import java.util.Deque;

/**
 * Per-channel auto-read save/restore.
 * <p>
 * Every transition runs on the channel's own event loop. The saved states are plain,
 * non-thread-safe per-channel state, and the callers do not agree on a thread:
 * {@code CompressionPacketHandler} restores from a listener on the client (c2p) channel's
 * event loop while {@code LoginPacketHandler} and {@code ConfigurationPacketHandler} disable
 * and restore from the backend (p2s) channel's event loop. Two restores running on two loops
 * pop the same stack, and whichever {@code setAutoRead(poppedValue)} lands last wins. When the
 * inner {@code false} wins, the channel stops reading for good: no exception is raised (both
 * restores see {@code autoRead == false}, so neither guard trips), the kernel receive queue
 * grows and {@code FlowControlHandler} stops firing packets it already decoded. Confining the
 * transitions to one loop makes the sequence of pushes and pops total, so the last restore
 * always restores the outermost state.
 */
public class ChannelUtil {

    private static final AttributeKey<Deque<Boolean>> LAST_AUTO_READ = AttributeKey.valueOf("last-auto-read");

    public static void disableAutoRead(final Channel channel) {
        onEventLoop(channel, () -> {
            Deque<Boolean> saved = channel.attr(LAST_AUTO_READ).get();
            if (saved == null) {
                saved = new ArrayDeque<>(2);
                channel.attr(LAST_AUTO_READ).set(saved);
            }

            saved.push(channel.config().isAutoRead());
            channel.config().setAutoRead(false);
        });
    }

    public static void restoreAutoRead(final Channel channel) {
        onEventLoop(channel, () -> {
            final Deque<Boolean> saved = channel.attr(LAST_AUTO_READ).get();
            if (saved == null || saved.isEmpty()) {
                throw new IllegalStateException("Tried to restore auto read, but it was never disabled");
            }
            if (channel.config().isAutoRead()) {
                throw new IllegalStateException("Race condition detected: Auto read has been enabled somewhere else");
            }
            channel.config().setAutoRead(saved.pop());
        });
    }

    private static void onEventLoop(final Channel channel, final Runnable transition) {
        final EventLoop eventLoop = channel.eventLoop();
        if (eventLoop.inEventLoop()) {
            transition.run();
        } else {
            eventLoop.execute(transition);
        }
    }

}
