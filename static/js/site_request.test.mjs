import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { createSiteRequestTracker } from "./site_request.js";

describe("site request tracker", () => {
    it("aborts the initial site load when another site is selected", () => {
        const tracker = createSiteRequestTracker();
        const initialRequest = tracker.begin("site-a");
        const switchRequest = tracker.begin("site-b");
        const committedSites = [];
        const commitIfCurrent = request => {
            if (tracker.isCurrent(request)) committedSites.push(request.siteID);
        };

        assert.equal(initialRequest.controller.signal.aborted, true);
        assert.equal(tracker.isCurrent(initialRequest), false);
        assert.equal(tracker.isCurrent(switchRequest), true);
        commitIfCurrent(initialRequest);
        commitIfCurrent(switchRequest);
        assert.deepEqual(committedSites, ["site-b"]);
    });

    it("aborts an existing site request and keeps only the latest request current", () => {
        const tracker = createSiteRequestTracker();
        const firstRequest = tracker.begin("site-a");
        const secondRequest = tracker.begin("site-b");
        const latestRequest = tracker.begin("site-c");
        const committedSites = [];
        const commitIfCurrent = request => {
            if (tracker.isCurrent(request)) committedSites.push(request.siteID);
        };

        assert.equal(firstRequest.controller.signal.aborted, true);
        assert.equal(secondRequest.controller.signal.aborted, true);
        assert.equal(latestRequest.controller.signal.aborted, false);
        assert.equal(tracker.isCurrent(firstRequest), false);
        assert.equal(tracker.isCurrent(secondRequest), false);
        assert.equal(tracker.isCurrent(latestRequest), true);
        commitIfCurrent(firstRequest);
        commitIfCurrent(secondRequest);
        commitIfCurrent(latestRequest);
        assert.deepEqual(committedSites, ["site-c"]);

        tracker.finish(secondRequest);
        assert.equal(tracker.isCurrent(latestRequest), true);
    });
});
