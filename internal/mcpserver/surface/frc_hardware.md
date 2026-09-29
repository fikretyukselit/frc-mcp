FRC hardware specs for sizing mechanisms, configuring swerve and sensors, and setting up simulation. Motors: stall torque, stall current, free speed, free current, nominal voltage (plus Kv, peak power, weight where a source gives them). Swerve modules: steering ratio, wheel diameter, weight. Encoders: counts per revolution, absolute resolution in bits, supply range. IMUs: drift, update rate, supply range. Every source is returned as its own column and never merged or averaged: "wpilib-dcmotor" is exactly what WPILib simulation uses (with the matching DCMotor factory in Java, C++ and Python); "recalc" is ReCalc's table, mostly vendor dyno data; "<vendor>-docs" is a vendor spec page; a plain vendor label (sds, rev, ctre, redux, wcp) is curated from the cited vendor page. Unit is the key suffix (_nm, _rpm, _a, _lb, _in, _bits, _deg_per_hour); ratios are dimensionless. Exact facts from a table, not a similarity search.

Use when: choosing a motor or gearing, estimating mechanism speed/torque, setting up a physics simulation (which DCMotor factory to call), or looking up a swerve module's steering ratio, an encoder's resolution or a gyro's drift. Accepts common names: "kraken", "kraken x60 foc", "neo", "vortex", "falcon", "neo550", "minion", "mk4i", "maxswerve", "cancoder", "through bore", "pigeon 2", "canandgyro".
Don't use when: you need motor controller configuration or API usage — use frc_search / frc_api. Values differ between sources (vendor dynos vs WPILib constants); cite the source you use. Swerve drive ratios are not listed (vendors publish them only as images); read the cited page.

Examples:
{"parts": ["kraken x60", "kraken x60 foc", "neo vortex"]}
{"category": "swerve_module"}
{"parts": ["neo"], "frc_season": "2027"}
