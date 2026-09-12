export function createSiteRequestTracker(controllerFactory = () => new AbortController()) {
    let generation = 0;
    let activeRequest = null;

    return {
        begin(siteID) {
            activeRequest?.controller.abort();
            const request = {
                generation: ++generation,
                siteID,
                controller: controllerFactory(),
            };
            activeRequest = request;
            return request;
        },

        isCurrent(request) {
            return Boolean(
                request &&
                request.generation === generation &&
                activeRequest === request
            );
        },

        finish(request) {
            if (activeRequest === request) activeRequest = null;
        },

        get generation() {
            return generation;
        },
    };
}
