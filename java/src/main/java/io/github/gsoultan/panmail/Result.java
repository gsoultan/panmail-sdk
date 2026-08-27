package io.github.gsoultan.panmail;

/**
 * What the gateway accepted.
 *
 * @param messageId identifies the message for the rest of its life: delivery
 *                  events, webhook notifications and the analytics pages are
 *                  all keyed by it. Worth storing next to whatever prompted
 *                  the send.
 * @param status    the status at the moment of acceptance, which for a queued
 *                  message is {@link Status#PENDING}.
 */
public record Result(String messageId, String status) {
}
