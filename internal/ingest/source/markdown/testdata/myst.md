# MultiTag Localization

PhotonVision can combine AprilTag detections from multiple simultaneously observed AprilTags.

:::{warning}
MultiTag requires an accurate field layout JSON to be uploaded!
:::

## Enabling MultiTag

Ensure that your camera is calibrated and 3D mode is enabled.

```{image} images/multitag-ui.png
:alt: Multitarget enabled
:width: 600
```

We suggest using {ref}`the PhotonPoseEstimator class <docs/programming/photonlib/robot-pose-estimator:AprilTags and PhotonPoseEstimator>` and calling `estimateCoprocMultiTagPose`.

```{eval-rst}
.. tab-set-code::

    .. code-block:: java

        var results = camera.getAllUnreadResults();
        for (var result : results) {
          var multiTagResult = result.getMultiTagResult();
        }


    .. code-block:: c++

      auto results = camera.GetAllUnreadResults();


    .. code-block:: python

      results = camera.getAllUnreadResults()
```
