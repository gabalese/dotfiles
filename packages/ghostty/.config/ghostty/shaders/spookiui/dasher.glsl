#define SPOOKI_TIME (iTime - iTimeFocus)
const float SPOOKI_VIBRANCY = 1.00;
const float SPOOKI_DIM = 1.0;

// added by SpookiUI: the treat animation only ever plays in the focused window.
// `iFocus` is 1.0 while focused, 0.0 while unfocused. When this window is
// unfocused we hide the effect and either:
//   - dim + desaturate the terminal (SPOOKI_DIM baked in), so the inactive
//     window is set apart from the focused one; or
//   - render the plain, untouched terminal when dimming is off.
vec4 spooki_out(vec3 res, vec4 term) {
    if (iFocus < 0.5) {
        if (SPOOKI_DIM > 0.5) {
            float g = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));  // luminance
            vec3 inactive = mix(term.rgb, vec3(g), 0.55) * 0.55;    // desaturate, dim
            return vec4(inactive, term.a);
        }
        return term;                                               // plain, no effect
    }
    vec3 fx = term.rgb + (res - term.rgb) * SPOOKI_VIBRANCY;    // scale the effect
    return vec4(min(fx, vec3(1.0)), term.a);
}

// SpookiUI treat: Dasher.
// A blue speedster curls into a spin-dash and blazes across a checkered green
// hill, scattering spinning gold rings and trailing speed-blur — a nostalgic
// nod to 90s blue-blur speed platformers. Original SpookiUI shader: plain
// shapes, no game artwork. Drawn over dark background pixels only.

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;
    vec2 p = vec2(uv.x * aspect, uv.y);

    vec3 acc = vec3(0.0);

    // Green-hill checkerboard ground. Screen y grows downward, so the ridge
    // sits near the bottom (large y) and the terrain fills the pixels below it.
    float ground = 0.86;
    float below = smoothstep(0.0, 0.006, p.y - ground);
    float scroll = SPOOKI_TIME * 0.6;                          // terrain streams left
    vec2 cell = vec2((p.x + scroll) * 14.0, (p.y - ground) * 10.0);
    float checker = mod(floor(cell.x) + floor(cell.y), 2.0);
    acc += mix(vec3(0.10, 0.45, 0.14), vec3(0.06, 0.30, 0.10), checker) * below;
    // A brighter grassy lip along the top edge of the ridge.
    acc += vec3(0.35, 0.75, 0.25)
         * (1.0 - smoothstep(0.003, 0.010, abs(p.y - ground)));

    // Spinning gold rings drifting left; each spins edge-on and back (its
    // x-radius collapses toward 0 as it turns side-on, the classic ring spin).
    const int R = 4;
    float ringw = aspect + 0.3;
    for (int i = 0; i < R; i++) {
        float fi = float(i);
        float rx = fract(fi / float(R) - SPOOKI_TIME * 0.11) * ringw - 0.15;
        float ry = 0.40 + 0.10 * sin(fi * 2.3);
        float spin = abs(cos(SPOOKI_TIME * 3.0 + fi * 1.7));   // 0 = seen edge-on
        vec2 rc = (p - vec2(rx, ry)) / vec2(0.030 * (0.15 + 0.85 * spin), 0.036);
        float rr = length(rc);
        float ring = smoothstep(1.0, 0.82, rr) * smoothstep(0.45, 0.63, rr);
        acc += vec3(1.0, 0.82, 0.15) * ring;
        acc += vec3(1.0, 0.97, 0.70) * ring * smoothstep(0.9, 1.0, spin);  // face shine
    }

    // The blue speedster: a spin-dashing ball hugging the ground.
    float dashw = aspect + 0.2;
    float bx = fract(SPOOKI_TIME * 0.33) * dashw - 0.1;
    float by = ground - 0.045 + 0.006 * sin(SPOOKI_TIME * 20.0);   // low jitter/hop
    vec2 c = p - vec2(bx, by);
    float br = 0.034;
    float ball = 1.0 - smoothstep(br - 0.004, br, length(c));
    // A pinwheel of light spinning inside the ball reads as motion.
    float ang = atan(c.y, c.x) + SPOOKI_TIME * 26.0;
    float swirl = 0.55 + 0.45 * sin(ang * 3.0);
    acc += mix(vec3(0.10, 0.20, 0.85), vec3(0.30, 0.55, 1.0), swirl) * ball;
    // A bright leading edge in the direction of travel.
    acc += vec3(0.70, 0.90, 1.0) * ball
         * smoothstep(0.3, 1.0, (c.x / br) * 0.5 + 0.5);

    // Speed-blur: a few horizontal streaks trailing behind the ball.
    for (int i = 0; i < 4; i++) {
        float fi = float(i);
        float ly = by + (fi - 1.5) * 0.015;
        float len = 0.10 + 0.03 * fi;
        float xin = smoothstep(bx - len, bx - 0.03, p.x);        // fade in from afar
        float xout = smoothstep(bx + 0.005, bx - 0.03, p.x);     // stop at the ball
        float yline = 1.0 - smoothstep(0.0015, 0.005, abs(p.y - ly));
        float shimmer = 0.5 + 0.5 * sin(SPOOKI_TIME * 18.0 - fi * 1.3);
        acc += vec3(0.70, 0.85, 1.0) * xin * xout * yline * (0.35 + 0.3 * shimmer);
    }

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.15, lum);
    vec3 res = term.rgb + acc * 0.6 * bgmask;
    fragColor = spooki_out(res, term);
}
