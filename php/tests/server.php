<?php

declare(strict_types=1);

/**
 * Router for the built-in server that CurlTransportTest drives. Only exists to
 * exercise the two things the transport does that a fake cannot show: metering
 * a large body, and declining to follow a redirect.
 */
$path = parse_url($_SERVER['REQUEST_URI'] ?? '/', PHP_URL_PATH);
$marker = sys_get_temp_dir() . '/panmail-followed-' . ($_GET['run'] ?? 'x');

switch ($path) {
    case '/ok':
        header('Content-Type: application/json');
        echo json_encode(['messageId' => 'msg_01', 'status' => 'EMAIL_EVENT_TYPE_PENDING']);

        return true;

    case '/huge':
        header('Content-Type: application/json');
        // Comfortably past the 1 MiB cap, streamed so the server does not have
        // to hold it either.
        for ($i = 0; $i < 24; $i++) {
            echo str_repeat('x', 64 * 1024);
            flush();
        }

        return true;

    case '/redirect':
        header('Location: /followed?' . ($_SERVER['QUERY_STRING'] ?? ''), true, 307);

        return true;

    case '/followed':
        // Records that the redirect was followed, and with which key.
        file_put_contents($marker, $_SERVER['HTTP_X_API_KEY'] ?? '(none)');
        header('Content-Type: application/json');
        echo json_encode(['messageId' => 'leaked']);

        return true;
}

http_response_code(404);

return true;
